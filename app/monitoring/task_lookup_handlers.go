package monitoring

import (
	"context"
	"errors"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/gorilla/mux"
	"github.com/redis/go-redis/v9"

	"github.com/Publikey/runqy/third_party/asynq"
)

// ****************************************************************************
// This file defines:
//   - GET /api/tasks/{task_id}: one task, whatever its queue (live task page)
//   - GET /api/tasks:           tasks of one state across all queues
// ****************************************************************************

// Task-hash fields read next to the inspector's TaskInfo (not part of it).
// pending_since is UnixNano; the limits are seconds stamped by the server at enqueue.
var taskExtraFields = []string{"pending_since", "ttl_completed", "ttl_archived", "pending_timeout", "active_timeout"}

type taskWorker struct {
	ID       string `json:"id"`
	Started  string `json:"started"`
	Deadline string `json:"deadline,omitempty"`
}

// taskDetail is the per-queue task detail (toTaskInfo) plus what the live task page needs.
type taskDetail struct {
	*taskInfo
	Group        string `json:"group,omitempty"`
	IsOrphaned   bool   `json:"is_orphaned"`
	PendingSince string `json:"pending_since,omitempty"`
	// Lifecycle limits stamped on the task (seconds), only those present.
	Limits map[string]int64 `json:"limits,omitempty"`
	// Remaining lifetime of the task hash in Redis (seconds); omitted when it has no expiry.
	KeyTTL *int64 `json:"key_ttl_seconds,omitempty"`
	// Set when the task is active and an asynq server reports it in its active workers.
	Worker *taskWorker `json:"worker,omitempty"`
}

type taskListItem struct {
	*taskInfo
	Group        string `json:"group,omitempty"`
	IsOrphaned   bool   `json:"is_orphaned"`
	PendingSince string `json:"pending_since,omitempty"`
}

type listAllTasksResponse struct {
	Tasks []*taskListItem `json:"tasks"`
	Total int             `json:"total"`
	Page  int             `json:"page"`
	Size  int             `json:"size"`
}

func taskHashKey(queue, id string) string {
	return "asynq:{" + queue + "}:t:" + id
}

// findTaskQueue resolves the queue holding a task: the server's reverse lookup first
// (asynq:t:<id>), then a scan of the known queues (tasks enqueued outside the server,
// or whose reverse lookup expired). Returns "" when no queue holds it.
func findTaskQueue(ctx context.Context, rc redis.UniversalClient, inspector *asynq.Inspector, id string) (string, error) {
	q, err := rc.HGet(ctx, "asynq:t:"+id, "queue").Result()
	if err != nil && !errors.Is(err, redis.Nil) {
		return "", err
	}
	if q != "" {
		if n, err := rc.Exists(ctx, taskHashKey(q, id)).Result(); err == nil && n > 0 {
			return q, nil
		}
	}
	queues, err := inspector.Queues()
	if err != nil {
		return "", err
	}
	for _, qname := range queues {
		if qname == q {
			continue // already checked
		}
		n, err := rc.Exists(ctx, taskHashKey(qname, id)).Result()
		if err != nil {
			return "", err
		}
		if n > 0 {
			return qname, nil
		}
	}
	return "", nil
}

func parseNanoTime(s string) string {
	n, err := strconv.ParseInt(s, 10, 64)
	if err != nil || n <= 0 {
		return ""
	}
	return time.Unix(0, n).Format(time.RFC3339)
}

func newGetTaskByIDHandlerFunc(rc redis.UniversalClient, inspector *asynq.Inspector, pf PayloadFormatter, rf ResultFormatter) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := mux.Vars(r)["task_id"]
		if id == "" {
			writeJSONError(w, "task_id cannot be empty", http.StatusBadRequest)
			return
		}
		ctx := r.Context()
		qname, err := findTaskQueue(ctx, rc, inspector, id)
		if err != nil {
			writeJSONError(w, err.Error(), http.StatusInternalServerError)
			return
		}
		if qname == "" {
			writeJSONError(w, "task not found", http.StatusNotFound)
			return
		}
		info, err := inspector.GetTaskInfo(qname, id)
		switch {
		case errors.Is(err, asynq.ErrQueueNotFound), errors.Is(err, asynq.ErrTaskNotFound):
			writeJSONError(w, "task not found", http.StatusNotFound)
			return
		case err != nil:
			writeJSONError(w, strings.TrimPrefix(err.Error(), "asynq: "), http.StatusInternalServerError)
			return
		}

		resp := &taskDetail{
			taskInfo:   toTaskInfo(info, pf, rf),
			Group:      info.Group,
			IsOrphaned: info.IsOrphaned,
		}
		key := taskHashKey(qname, id)
		if vals, err := rc.HMGet(ctx, key, taskExtraFields...).Result(); err == nil {
			for i, v := range vals {
				s, ok := v.(string)
				if !ok || s == "" {
					continue
				}
				if i == 0 {
					resp.PendingSince = parseNanoTime(s)
					continue
				}
				if n, err := strconv.ParseInt(s, 10, 64); err == nil {
					if resp.Limits == nil {
						resp.Limits = make(map[string]int64)
					}
					resp.Limits[taskExtraFields[i]] = n
				}
			}
		}
		if ttl, err := rc.TTL(ctx, key).Result(); err == nil && ttl > 0 {
			secs := int64(ttl.Seconds())
			resp.KeyTTL = &secs
		}
		if info.State == asynq.TaskStateActive {
			// Only asynq-native servers publish their in-flight tasks; runqy workers do not
			// (yet), in which case the worker stays unknown.
			if servers, err := inspector.Servers(); err == nil {
			outer:
				for _, srv := range servers {
					for _, aw := range srv.ActiveWorkers {
						if aw.TaskID == id {
							resp.Worker = &taskWorker{
								ID:       srv.ID,
								Started:  formatTimeInRFC3339(aw.Started),
								Deadline: formatTimeInRFC3339(aw.Deadline),
							}
							break outer
						}
					}
				}
			}
		}
		writeResponseJSON(w, resp)
	}
}

// Upper bound on the tasks listed per queue for one request (page*size), so a deep page
// cannot make the server load an unbounded number of tasks.
const maxTasksPerQueue = 5000

var listableStates = map[string]bool{
	"pending": true, "active": true, "scheduled": true, "retry": true, "archived": true, "completed": true,
}

func listTasksInState(inspector *asynq.Inspector, qname, state string, n int) ([]*asynq.TaskInfo, error) {
	opts := []asynq.ListOption{asynq.PageSize(n), asynq.Page(1)}
	switch state {
	case "pending":
		return inspector.ListPendingTasks(qname, opts...)
	case "active":
		return inspector.ListActiveTasks(qname, opts...)
	case "scheduled":
		return inspector.ListScheduledTasks(qname, opts...)
	case "retry":
		return inspector.ListRetryTasks(qname, opts...)
	case "archived":
		return inspector.ListArchivedTasks(qname, opts...)
	default:
		return inspector.ListCompletedTasks(qname, opts...)
	}
}

func stateCount(info *asynq.QueueInfo, state string) int {
	switch state {
	case "pending":
		return info.Pending
	case "active":
		return info.Active
	case "scheduled":
		return info.Scheduled
	case "retry":
		return info.Retry
	case "archived":
		return info.Archived
	default:
		return info.Completed
	}
}

// newListAllTasksHandlerFunc lists the tasks of one state across every queue (or one).
//
// Cost: there is no global index of tasks, so a page is built by listing the first
// page*size tasks of the state in every queue that has some (queues at zero are skipped
// using their stats), merging and sorting them in memory, then slicing the page.
// Per request: one stats call per queue plus up to page*size tasks per non-empty queue
// (capped at maxTasksPerQueue), i.e. O(size × page × queues). Fine for a dashboard with
// a handful of queues and shallow pages; deep pages on many busy queues get expensive.
func newListAllTasksHandlerFunc(rc redis.UniversalClient, inspector *asynq.Inspector, pf PayloadFormatter, rf ResultFormatter) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		state := r.URL.Query().Get("state")
		if state == "" {
			state = "active"
		}
		if !listableStates[state] {
			writeJSONError(w, "invalid state: "+state, http.StatusBadRequest)
			return
		}
		size, page := getPageOptions(r)
		if size < 1 {
			size = 20
		}
		if size > 100 {
			size = 100
		}
		if page < 1 {
			page = 1
		}
		want := page * size
		if want > maxTasksPerQueue {
			want = maxTasksPerQueue
		}

		var queues []string
		if q := r.URL.Query().Get("queue"); q != "" {
			queues = []string{q}
		} else {
			all, err := inspector.Queues()
			if err != nil {
				writeJSONError(w, err.Error(), http.StatusInternalServerError)
				return
			}
			queues = all
		}
		sort.Strings(queues)

		total := 0
		var all []*asynq.TaskInfo
		for _, qname := range queues {
			info, err := inspector.GetQueueInfo(qname)
			if err != nil {
				if errors.Is(err, asynq.ErrQueueNotFound) {
					continue
				}
				writeJSONError(w, err.Error(), http.StatusInternalServerError)
				return
			}
			n := stateCount(info, state)
			total += n
			if n == 0 {
				continue
			}
			tasks, err := listTasksInState(inspector, qname, state, want)
			if err != nil {
				if errors.Is(err, asynq.ErrQueueNotFound) {
					continue
				}
				writeJSONError(w, err.Error(), http.StatusInternalServerError)
				return
			}
			all = append(all, tasks...)
		}

		// pending_since is not part of TaskInfo: read it for the pending state only.
		pendingSince := make(map[string]int64)
		if state == "pending" && len(all) > 0 {
			ctx := r.Context()
			pipe := rc.Pipeline()
			cmds := make([]*redis.StringCmd, len(all))
			for i, t := range all {
				cmds[i] = pipe.HGet(ctx, taskHashKey(t.Queue, t.ID), "pending_since")
			}
			_, _ = pipe.Exec(ctx) // missing fields come back as redis.Nil
			for i, c := range cmds {
				if n, err := strconv.ParseInt(c.Val(), 10, 64); err == nil && n > 0 {
					pendingSince[all[i].Queue+"\x00"+all[i].ID] = n
				}
			}
		}

		sortTasks(all, state, pendingSince)

		start := (page - 1) * size
		end := start + size
		if start > len(all) {
			start = len(all)
		}
		if end > len(all) {
			end = len(all)
		}
		items := make([]*taskListItem, 0, end-start)
		for _, t := range all[start:end] {
			item := &taskListItem{taskInfo: toTaskInfo(t, pf, rf), Group: t.Group, IsOrphaned: t.IsOrphaned}
			if ns, ok := pendingSince[t.Queue+"\x00"+t.ID]; ok {
				item.PendingSince = time.Unix(0, ns).Format(time.RFC3339)
			}
			items = append(items, item)
		}
		writeResponseJSON(w, listAllTasksResponse{Tasks: items, Total: total, Page: page, Size: size})
	}
}

// sortTasks orders merged tasks: completed newest first, retry/archived by last failure
// (newest first), scheduled by next run (soonest first), pending oldest first when
// pending_since is known; active keeps queue order. Stable, so ties keep queue order.
func sortTasks(tasks []*asynq.TaskInfo, state string, pendingSince map[string]int64) {
	switch state {
	case "completed":
		sort.SliceStable(tasks, func(i, j int) bool { return tasks[i].CompletedAt.After(tasks[j].CompletedAt) })
	case "retry", "archived":
		sort.SliceStable(tasks, func(i, j int) bool { return tasks[i].LastFailedAt.After(tasks[j].LastFailedAt) })
	case "scheduled":
		sort.SliceStable(tasks, func(i, j int) bool { return tasks[i].NextProcessAt.Before(tasks[j].NextProcessAt) })
	case "pending":
		sort.SliceStable(tasks, func(i, j int) bool {
			a, aok := pendingSince[tasks[i].Queue+"\x00"+tasks[i].ID]
			b, bok := pendingSince[tasks[j].Queue+"\x00"+tasks[j].ID]
			switch {
			case aok && bok:
				return a < b
			case aok != bok:
				return aok // known pending_since first
			default:
				return false
			}
		})
	}
}
