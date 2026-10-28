package monitoring

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/Publikey/runqy/control"
	"github.com/gorilla/mux"
	"github.com/redis/go-redis/v9"
)

// ****************************************************************************
// This file defines:
//   - http.Handler(s) for external workers (runqy-worker) endpoints
//   - Reads from asynq:workers:* keys (HASH type) set by runqy-worker
// ****************************************************************************

// systemMetrics matches the worker's SystemMetrics struct.
type systemMetrics struct {
	CPUPercent       float64      `json:"cpu_percent"`
	MemoryUsedBytes  uint64       `json:"memory_used_bytes"`
	MemoryTotalBytes uint64       `json:"memory_total_bytes"`
	GPUs             []gpuMetrics `json:"gpus,omitempty"`
	CollectedAt      int64        `json:"collected_at"`
}

type gpuMetrics struct {
	Index              int     `json:"index"`
	Name               string  `json:"name"`
	UtilizationPercent float64 `json:"utilization_percent"`
	MemoryUsedMB       uint64  `json:"memory_used_mb"`
	MemoryTotalMB      uint64  `json:"memory_total_mb"`
	TemperatureC       int     `json:"temperature_c"`
}

// workerData represents an external worker from asynq:workers:* keys
type workerData struct {
	WorkerID    string         `json:"worker_id"`
	StartedAt   int64          `json:"started_at"`
	LastBeat    int64          `json:"last_beat"`
	Concurrency int            `json:"concurrency"`
	Queues      string         `json:"queues"`
	Status      string         `json:"status"`
	IsStale     bool           `json:"is_stale"` // True if no heartbeat for 30+ seconds
	Metrics     *systemMetrics `json:"metrics,omitempty"`
	InstanceID  string         `json:"instance_id,omitempty"`
	control.WorkerFields
}

type listWorkersResponse struct {
	Workers []*workerData `json:"workers"`
}

// newListWorkersHandlerFunc returns a handler that lists external workers.
// External workers (like runqy-worker) register themselves at asynq:workers:{id} keys.
func newListWorkersHandlerFunc(rc redis.UniversalClient) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx := context.Background()

		// Scan for all worker keys matching pattern asynq:workers:*
		var workerKeys []string
		iter := rc.Scan(ctx, 0, "asynq:workers:*", 100).Iterator()
		for iter.Next(ctx) {
			key := iter.Val()
			// Skip if it's not a worker data key (must have at least 2 colons)
			// Also skip log keys (asynq:workers:{id}:logs)
			if strings.Count(key, ":") >= 2 && !strings.HasSuffix(key, ":logs") {
				workerKeys = append(workerKeys, key)
			}
		}
		if err := iter.Err(); err != nil {
			writeJSONError(w, err.Error(), http.StatusInternalServerError)
			return
		}

		workers := make([]*workerData, 0, len(workerKeys))
		now := time.Now().Unix()

		for _, workerKey := range workerKeys {
			// Check if it's a HASH (worker data) - skip other types
			keyType, err := rc.Type(ctx, workerKey).Result()
			if err != nil || keyType != "hash" {
				continue
			}

			data, err := rc.HGetAll(ctx, workerKey).Result()
			if err != nil || len(data) == 0 {
				continue
			}

			// Extract worker ID from key (asynq:workers:{worker_id})
			workerID := strings.TrimPrefix(workerKey, "asynq:workers:")

			worker := &workerData{
				WorkerID:     workerID,
				Status:       data["status"],
				Queues:       data["queues"],
				InstanceID:   data["instance_id"],
				WorkerFields: control.ParseWorkerFields(data),
			}

			if startedAt, err := strconv.ParseInt(data["started_at"], 10, 64); err == nil {
				worker.StartedAt = startedAt
			}
			if lastBeat, err := strconv.ParseInt(data["last_beat"], 10, 64); err == nil {
				worker.LastBeat = lastBeat
				// Consider stale if no heartbeat for 30 seconds
				worker.IsStale = (now - lastBeat) > 30
			}
			if concurrency, err := strconv.Atoi(data["concurrency"]); err == nil {
				worker.Concurrency = concurrency
			}

			// Parse metrics JSON if present
			if metricsJSON, ok := data["metrics"]; ok && metricsJSON != "" {
				var m systemMetrics
				if json.Unmarshal([]byte(metricsJSON), &m) == nil {
					worker.Metrics = &m
				}
			}

			workers = append(workers, worker)
		}

		resp := listWorkersResponse{Workers: workers}
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(resp); err != nil {
			writeJSONError(w, err.Error(), http.StatusInternalServerError)
			return
		}
	}
}

// newGetWorkerLogsHandlerFunc returns a handler that gets recent log lines for a worker.
func newGetWorkerLogsHandlerFunc(rc redis.UniversalClient) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		vars := mux.Vars(r)
		workerID := vars["workerId"]
		if workerID == "" {
			writeJSONError(w, "workerId is required", http.StatusBadRequest)
			return
		}

		ctx := context.Background()
		logKey := fmt.Sprintf("asynq:workers:%s:logs", workerID)

		// Get last N lines (default 200)
		n := int64(200)
		if nStr := r.URL.Query().Get("n"); nStr != "" {
			if parsed, err := strconv.ParseInt(nStr, 10, 64); err == nil && parsed > 0 {
				n = parsed
			}
		}

		lines, err := rc.LRange(ctx, logKey, -n, -1).Result()
		if err != nil {
			writeJSONError(w, err.Error(), http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"lines": lines,
			"count": len(lines),
		})
	}
}

// newWorkerLogStreamHandlerFunc returns an SSE handler that streams worker logs.
func newWorkerLogStreamHandlerFunc(rc redis.UniversalClient) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		vars := mux.Vars(r)
		workerID := vars["workerId"]
		if workerID == "" {
			writeJSONError(w, "workerId is required", http.StatusBadRequest)
			return
		}

		flusher, ok := w.(http.Flusher)
		if !ok {
			writeJSONError(w, "streaming not supported", http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("Connection", "keep-alive")
		w.Header().Set("Access-Control-Allow-Origin", "*")

		ctx := r.Context()
		logKey := fmt.Sprintf("asynq:workers:%s:logs", workerID)
		ticker := time.NewTicker(2 * time.Second)
		defer ticker.Stop()

		lastLen := int64(0)

		// Send initial lines
		currentLen, _ := rc.LLen(ctx, logKey).Result()
		if currentLen > 0 {
			// Send last 100 lines initially
			start := currentLen - 100
			if start < 0 {
				start = 0
			}
			lines, _ := rc.LRange(ctx, logKey, start, currentLen-1).Result()
			for _, line := range lines {
				fmt.Fprintf(w, "data: %s\n\n", line)
			}
			flusher.Flush()
			lastLen = currentLen
		}

		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				currentLen, err := rc.LLen(ctx, logKey).Result()
				if err != nil {
					continue
				}
				if currentLen > lastLen {
					newLines, err := rc.LRange(ctx, logKey, lastLen, currentLen-1).Result()
					if err != nil {
						continue
					}
					for _, line := range newLines {
						fmt.Fprintf(w, "data: %s\n\n", line)
					}
					flusher.Flush()
					lastLen = currentLen
				} else if currentLen < lastLen {
					// List was trimmed, reset
					lastLen = currentLen
				}
			}
		}
	}
}
