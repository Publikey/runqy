package monitoring

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/gorilla/mux"
	"github.com/redis/go-redis/v9"

	"github.com/Publikey/runqy/third_party/asynq"
)

type lookupEnv struct {
	rdb       *redis.Client
	client    *asynq.Client
	inspector *asynq.Inspector
	router    *mux.Router
}

func newLookupEnv(t *testing.T) *lookupEnv {
	t.Helper()
	mr := miniredis.RunT(t)
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { rdb.Close() })
	inspector := asynq.NewInspectorFromRedisClient(rdb)
	router := mux.NewRouter()
	router.HandleFunc("/api/tasks", newListAllTasksHandlerFunc(rdb, inspector, DefaultPayloadFormatter, DefaultResultFormatter)).Methods("GET")
	router.HandleFunc("/api/tasks/{task_id}", newGetTaskByIDHandlerFunc(rdb, inspector, DefaultPayloadFormatter, DefaultResultFormatter)).Methods("GET")
	return &lookupEnv{rdb: rdb, client: asynq.NewClientFromRedisClient(rdb), inspector: inspector, router: router}
}

func (e *lookupEnv) enqueue(t *testing.T, queue, id string, opts ...asynq.Option) {
	t.Helper()
	opts = append([]asynq.Option{asynq.Queue(queue), asynq.TaskID(id)}, opts...)
	if _, err := e.client.Enqueue(asynq.NewTask("task", []byte(`{"n":1}`)), opts...); err != nil {
		t.Fatalf("enqueue %s/%s: %v", queue, id, err)
	}
}

func (e *lookupEnv) get(t *testing.T, url string, out any) int {
	t.Helper()
	rec := httptest.NewRecorder()
	e.router.ServeHTTP(rec, httptest.NewRequest("GET", url, nil))
	if out != nil && rec.Code == http.StatusOK {
		if err := json.Unmarshal(rec.Body.Bytes(), out); err != nil {
			t.Fatalf("decode %s: %v (%s)", url, err, rec.Body.String())
		}
	}
	return rec.Code
}

func TestGetTaskByIDViaReverseLookup(t *testing.T) {
	e := newLookupEnv(t)
	e.enqueue(t, "video.high", "t-1")
	// Written by the server's client at enqueue.
	e.rdb.HSet(context.Background(), "asynq:t:t-1", "queue", "video.high")
	e.rdb.HSet(context.Background(), "asynq:{video.high}:t:t-1", "active_timeout", "30")

	var got map[string]any
	if code := e.get(t, "/api/tasks/t-1", &got); code != http.StatusOK {
		t.Fatalf("status %d", code)
	}
	if got["queue"] != "video.high" || got["state"] != "pending" || got["id"] != "t-1" {
		t.Fatalf("unexpected task: %v", got)
	}
	if got["payload"] != `{"n":1}` {
		t.Fatalf("payload %v", got["payload"])
	}
	limits, _ := got["limits"].(map[string]any)
	if limits["active_timeout"] != float64(30) {
		t.Fatalf("limits %v", got["limits"])
	}
	if _, ok := got["worker"]; ok {
		t.Fatalf("pending task must not have a worker: %v", got["worker"])
	}
}

func TestGetTaskByIDViaQueueScan(t *testing.T) {
	e := newLookupEnv(t)
	e.enqueue(t, "a.default", "other")
	e.enqueue(t, "b.default", "t-2", asynq.ProcessIn(time.Hour))
	// Stale reverse lookup pointing at the wrong queue: the scan must still find it.
	e.rdb.HSet(context.Background(), "asynq:t:t-2", "queue", "a.default")

	var got map[string]any
	if code := e.get(t, "/api/tasks/t-2", &got); code != http.StatusOK {
		t.Fatalf("status %d", code)
	}
	if got["queue"] != "b.default" || got["state"] != "scheduled" {
		t.Fatalf("unexpected task: %v", got)
	}
}

func TestGetTaskByIDUnknown(t *testing.T) {
	e := newLookupEnv(t)
	e.enqueue(t, "a.default", "exists")
	rec := httptest.NewRecorder()
	e.router.ServeHTTP(rec, httptest.NewRequest("GET", "/api/tasks/nope", nil))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status %d, want 404", rec.Code)
	}
	var body errorsResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil || len(body.Errors) != 1 || body.Errors[0] != "task not found" {
		t.Fatalf("body %s", rec.Body.String())
	}
}

func TestListAllTasksMergeSortPaginate(t *testing.T) {
	e := newLookupEnv(t)
	// Scheduled tasks interleaved across two queues: soonest first after the merge.
	e.enqueue(t, "a.default", "a1", asynq.ProcessIn(1*time.Hour))
	e.enqueue(t, "b.default", "b1", asynq.ProcessIn(2*time.Hour))
	e.enqueue(t, "a.default", "a2", asynq.ProcessIn(3*time.Hour))
	e.enqueue(t, "b.default", "b2", asynq.ProcessIn(4*time.Hour))
	e.enqueue(t, "a.default", "a3", asynq.ProcessIn(5*time.Hour))
	e.enqueue(t, "a.default", "p1") // pending: not in the scheduled list

	type resp struct {
		Tasks []struct {
			ID    string `json:"id"`
			Queue string `json:"queue"`
			State string `json:"state"`
		} `json:"tasks"`
		Total int `json:"total"`
		Page  int `json:"page"`
		Size  int `json:"size"`
	}
	ids := func(r resp) []string {
		out := []string{}
		for _, t := range r.Tasks {
			out = append(out, t.ID)
		}
		return out
	}
	check := func(url string, wantIDs []string, wantTotal int) resp {
		t.Helper()
		var r resp
		if code := e.get(t, url, &r); code != http.StatusOK {
			t.Fatalf("%s: status %d", url, code)
		}
		got := ids(r)
		if len(got) != len(wantIDs) {
			t.Fatalf("%s: ids %v, want %v", url, got, wantIDs)
		}
		for i := range got {
			if got[i] != wantIDs[i] {
				t.Fatalf("%s: ids %v, want %v", url, got, wantIDs)
			}
		}
		if r.Total != wantTotal {
			t.Fatalf("%s: total %d, want %d", url, r.Total, wantTotal)
		}
		return r
	}

	r := check("/api/tasks?state=scheduled&page=1&size=2", []string{"a1", "b1"}, 5)
	if r.Page != 1 || r.Size != 2 || r.Tasks[1].Queue != "b.default" || r.Tasks[0].State != "scheduled" {
		t.Fatalf("unexpected page: %+v", r)
	}
	check("/api/tasks?state=scheduled&page=2&size=2", []string{"a2", "b2"}, 5)
	check("/api/tasks?state=scheduled&page=3&size=2", []string{"a3"}, 5)
	check("/api/tasks?state=scheduled&page=4&size=2", []string{}, 5)
	check("/api/tasks?state=scheduled&queue=b.default", []string{"b1", "b2"}, 2)
	check("/api/tasks?state=pending", []string{"p1"}, 1)
	check("/api/tasks?state=active", []string{}, 0)

	if code := e.get(t, "/api/tasks?state=bogus", nil); code != http.StatusBadRequest {
		t.Fatalf("invalid state: status %d", code)
	}
}

func TestListAllTasksPendingSortedByPendingSince(t *testing.T) {
	e := newLookupEnv(t)
	e.enqueue(t, "a.default", "newer")
	e.enqueue(t, "b.default", "older")
	ctx := context.Background()
	now := time.Now()
	e.rdb.HSet(ctx, "asynq:{a.default}:t:newer", "pending_since", now.UnixNano())
	e.rdb.HSet(ctx, "asynq:{b.default}:t:older", "pending_since", now.Add(-time.Minute).UnixNano())

	var r struct {
		Tasks []struct {
			ID           string `json:"id"`
			PendingSince string `json:"pending_since"`
		} `json:"tasks"`
	}
	if code := e.get(t, "/api/tasks?state=pending", &r); code != http.StatusOK {
		t.Fatalf("status %d", code)
	}
	if len(r.Tasks) != 2 || r.Tasks[0].ID != "older" || r.Tasks[1].ID != "newer" || r.Tasks[0].PendingSince == "" {
		t.Fatalf("unexpected order: %+v", r.Tasks)
	}
}
