package monitoring

import (
	"encoding/json"
	"net/http"

	runqyclient "github.com/Publikey/runqy/client"
	queueworker "github.com/Publikey/runqy/queues"
	"github.com/Publikey/runqy/third_party/asynq"
	"github.com/redis/go-redis/v9"
)

// ****************************************************************************
// Playground: manually enqueue tasks from the dashboard.
// Uses the exact same enqueue path as the public POST /add API
// (client.EnqueueGenericTask), so playground tasks are indistinguishable
// from client-enqueued ones. Protected by the dashboard auth middleware,
// and blocked in read-only mode like every non-GET /api route.
// ****************************************************************************

const playgroundMaxCount = 100

type playgroundEnqueueRequest struct {
	Queue   string          `json:"queue"`
	Payload json.RawMessage `json:"payload"`
	Count   int             `json:"count"`
	Timeout int64           `json:"timeout"` // seconds; 0 = queue default
}

type playgroundTask struct {
	ID    string `json:"id"`
	Queue string `json:"queue"`
}

type playgroundEnqueueResponse struct {
	Tasks []playgroundTask `json:"tasks"`
	Count int              `json:"count"`
}

func newPlaygroundEnqueueHandlerFunc(asynqClient *asynq.Client, rdb *redis.Client, queueStore *queueworker.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req playgroundEnqueueRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeJSONError(w, "invalid request body: "+err.Error(), http.StatusBadRequest)
			return
		}
		if req.Queue == "" {
			writeJSONError(w, "queue is required", http.StatusBadRequest)
			return
		}
		if len(req.Payload) == 0 || !json.Valid(req.Payload) {
			writeJSONError(w, "payload must be valid JSON", http.StatusBadRequest)
			return
		}
		if req.Count <= 0 {
			req.Count = 1
		}
		if req.Count > playgroundMaxCount {
			writeJSONError(w, "count must be at most 100", http.StatusBadRequest)
			return
		}
		if req.Timeout < 0 {
			writeJSONError(w, "timeout must be >= 0", http.StatusBadRequest)
			return
		}

		// Resolve task lifecycle limits from the parent queue config
		// (server defaults ⊕ override), same as the public /add endpoint.
		limits := queueworker.DefaultLimits()
		if queueStore != nil {
			parentQueue, _, _ := queueworker.ParseQueueName(req.Queue)
			if pq, err := queueStore.GetQueue(r.Context(), parentQueue); err == nil && pq != nil {
				limits = queueworker.ResolveQueueLimits(pq)
			}
		}

		resp := playgroundEnqueueResponse{Tasks: make([]playgroundTask, 0, req.Count)}
		for i := 0; i < req.Count; i++ {
			info, err := runqyclient.EnqueueGenericTask(asynqClient, rdb, req.Queue, req.Timeout, req.Payload, limits)
			if err != nil {
				writeJSONError(w, err.Error(), http.StatusBadRequest)
				return
			}
			resp.Tasks = append(resp.Tasks, playgroundTask{ID: info.ID, Queue: info.Queue})
		}
		resp.Count = len(resp.Tasks)

		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(resp); err != nil {
			writeJSONError(w, err.Error(), http.StatusInternalServerError)
		}
	}
}
