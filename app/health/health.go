// Package health computes a point-in-time view of every queue and worker: pending and
// active jobs, latency (age of the oldest pending job), live and unhealthy workers.
// It is the shared input of the alert evaluator and the autoscaler.
package health

import (
	"context"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/Publikey/runqy/control"
	queueworker "github.com/Publikey/runqy/queues"
	"github.com/Publikey/runqy/third_party/asynq"
	"github.com/redis/go-redis/v9"
)

// StaleAfter is how old a worker heartbeat may be before the worker counts as gone.
// It matches the worker hash TTL and the dashboard's staleness rule.
const StaleAfter = 30 * time.Second

// Worker is one worker heartbeat (Redis hash asynq:workers:<id>).
type Worker struct {
	ID         string
	Queues     string // raw "queues" field, see queueworker.ParseWorkerQueues
	InstanceID string // autoscaler correlation id, empty for manual workers
	Status     string // "bootstrapping" | "running" | ...
	Healthy    bool
	Protected  bool
	LastBeat   int64 // unix seconds
	Active     int   // in-flight tasks; -1 when the worker does not report it (older workers)
	LastTaskAt int64 // unix seconds of last task start/finish; 0 if none or not reported
	Control    control.WorkerFields
}

// Fresh reports whether the worker heartbeated within StaleAfter of now.
func (w Worker) Fresh(now time.Time) bool {
	return now.Unix()-w.LastBeat <= int64(StaleAfter/time.Second)
}

// ParseWorker decodes a worker hash.
func ParseWorker(id string, data map[string]string) Worker {
	w := Worker{
		ID:         id,
		Queues:     data["queues"],
		InstanceID: data["instance_id"],
		Status:     data["status"],
		Healthy:    data["healthy"] != "0" && !strings.EqualFold(data["healthy"], "false"),
		Protected:  strings.EqualFold(data["protected"], "true"),
		Active:     -1,
	}
	if lb, err := strconv.ParseInt(data["last_beat"], 10, 64); err == nil {
		w.LastBeat = lb
	}
	if n, err := strconv.Atoi(data["active"]); err == nil {
		w.Active = n
	}
	if t, err := strconv.ParseInt(data["last_task_at"], 10, 64); err == nil {
		w.LastTaskAt = t
	}
	w.Control = control.ParseWorkerFields(data)
	return w
}

// ReadWorkers reads every worker heartbeat from Redis. Unlike a best-effort scan, it
// returns an error when the scan fails, so callers never mistake an outage for "no workers".
func ReadWorkers(ctx context.Context, rdb *redis.Client) ([]Worker, error) {
	iter := rdb.Scan(ctx, 0, "asynq:workers:*", 100).Iterator()
	var keys []string
	for iter.Next(ctx) {
		k := iter.Val()
		// Worker ids are "host:pid", so filter on the known sub-key suffix rather than
		// on the number of colons; any other non-hash key is skipped below (WRONGTYPE).
		if !strings.HasSuffix(k, ":logs") {
			keys = append(keys, k)
		}
	}
	if err := iter.Err(); err != nil {
		return nil, err
	}
	out := make([]Worker, 0, len(keys))
	for _, key := range keys {
		data, err := rdb.HGetAll(ctx, key).Result()
		if err != nil {
			if strings.Contains(err.Error(), "WRONGTYPE") {
				continue
			}
			return nil, err
		}
		if len(data) == 0 {
			continue // expired between SCAN and HGETALL
		}
		out = append(out, ParseWorker(strings.TrimPrefix(key, "asynq:workers:"), data))
	}
	return out, nil
}

// QueueHealth is the state of one parent queue, aggregated over its sub-queues.
type QueueHealth struct {
	Name             string                        `json:"name"`
	SubQueues        []string                      `json:"sub_queues"` // full names, e.g. inference.high
	Pending          int                           `json:"pending"`
	Active           int                           `json:"active"`
	Latency          time.Duration                 `json:"latency"` // oldest pending job across sub-queues
	Paused           bool                          `json:"paused"`  // every sub-queue is paused
	Workers          int                           `json:"workers"` // live workers serving the queue
	UnhealthyWorkers []string                      `json:"unhealthy_workers,omitempty"`
	Deployment       *queueworker.DeploymentConfig `json:"-"`
}

// QueueSource lists the configured queues.
type QueueSource interface {
	ListAllQueuesWithSubQueues(ctx context.Context) ([]queueworker.QueueWithSubQueues, error)
}

// QueueInspector returns live statistics for one full queue name.
type QueueInspector interface {
	GetQueueInfo(queue string) (*asynq.QueueInfo, error)
}

// Collector builds QueueHealth snapshots.
type Collector struct {
	queues    QueueSource
	inspector QueueInspector
	workers   func(ctx context.Context) ([]Worker, error)
}

// NewCollector creates a Collector reading workers from rdb.
func NewCollector(queues QueueSource, inspector QueueInspector, rdb *redis.Client) *Collector {
	return &Collector{
		queues:    queues,
		inspector: inspector,
		workers:   func(ctx context.Context) ([]Worker, error) { return ReadWorkers(ctx, rdb) },
	}
}

// Collect returns the health of every enabled parent queue, sorted by name.
func (c *Collector) Collect(ctx context.Context, now time.Time) ([]QueueHealth, error) {
	queues, err := c.queues.ListAllQueuesWithSubQueues(ctx)
	if err != nil {
		return nil, err
	}
	workers, err := c.workers(ctx)
	if err != nil {
		return nil, err
	}

	out := make([]QueueHealth, 0, len(queues))
	for _, q := range queues {
		if !q.Enabled {
			continue
		}
		qh := QueueHealth{Name: q.Name, Deployment: q.Deployment}
		subs := q.SubQueues
		if len(subs) == 0 {
			qh.SubQueues = []string{queueworker.NormalizeQueueName(q.Name)}
		}
		for _, sq := range subs {
			if sq.Enabled {
				qh.SubQueues = append(qh.SubQueues, queueworker.BuildFullQueueName(q.Name, sq.Name))
			}
		}

		pausedAll := len(qh.SubQueues) > 0
		for _, full := range qh.SubQueues {
			info, err := c.inspector.GetQueueInfo(full)
			if err != nil {
				pausedAll = false // queue not created in Redis yet: nothing pending
				continue
			}
			qh.Pending += info.Pending
			qh.Active += info.Active
			if info.Latency > qh.Latency {
				qh.Latency = info.Latency
			}
			if !info.Paused {
				pausedAll = false
			}
		}
		qh.Paused = pausedAll

		for _, w := range workers {
			if !w.Fresh(now) || !queueworker.WorkerServesParent(w.Queues, q.Name) {
				continue
			}
			qh.Workers++
			if !w.Healthy {
				qh.UnhealthyWorkers = append(qh.UnhealthyWorkers, w.ID)
			}
		}
		sort.Strings(qh.UnhealthyWorkers)
		out = append(out, qh)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}
