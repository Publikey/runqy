package control

import (
	"context"
	"encoding/json"
	"net/http"

	queueworker "github.com/Publikey/runqy/queues"
	"github.com/redis/go-redis/v9"
)

// API serves POST /api/control (admin only).
type API struct {
	Redis  *redis.Client
	Queues *queueworker.Store
}

// QueueExists reports whether a parent queue is configured.
func QueueExists(store *queueworker.Store) func(ctx context.Context, parent string) (bool, error) {
	return func(ctx context.Context, parent string) (bool, error) {
		q, err := store.GetQueue(ctx, parent)
		return q != nil, err
	}
}

// Send publishes a control command. Responds 202 with the channel and the number of
// subscribed receivers (0 means no worker listened: older workers do not subscribe).
func (a *API) Send(w http.ResponseWriter, r *http.Request) {
	var req Request
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64<<10)).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"errors": []string{"invalid JSON body: " + err.Error()}})
		return
	}
	res, err := Send(r.Context(), a.Redis, QueueExists(a.Queues), req)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"errors": []string{err.Error()}})
		return
	}
	writeJSON(w, http.StatusAccepted, res)
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
