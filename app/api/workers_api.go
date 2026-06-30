package api

import (
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/Publikey/runqy/models"
	"github.com/gin-gonic/gin"
	"github.com/redis/go-redis/v9"
)

// Redis key pattern for worker data (matching runqy-worker)
// runqy-worker stores worker data as: asynq:workers:{worker_id} (HASH)
const keyWorkerPattern = "asynq:workers:*"

// WorkerInfo represents worker information from Redis
type WorkerInfo struct {
	WorkerID    string `json:"worker_id"`
	StartedAt   int64  `json:"started_at"`
	LastBeat    int64  `json:"last_beat"`
	Concurrency int    `json:"concurrency"`
	Queues      string `json:"queues"`
	Status      string `json:"status"`
	IsStale     bool   `json:"is_stale"`              // True if last_beat > 30s ago
	InstanceID  string `json:"instance_id,omitempty"` // autoscaler correlation id (if autoscaler-managed)
	Protected   bool   `json:"protected"`             // worker self-declared protection from autoscaling
}

// WorkersResponse is the API response for listing workers
type WorkersResponse struct {
	Workers []WorkerInfo `json:"workers"`
	Count   int          `json:"count"`
}

// ListWorkers returns all registered workers
// @Summary List all workers
// @Description Returns all workers registered in Redis (from asynq:workers:* keys)
// @Tags workers
// @Produce json
// @Success 200 {object} WorkersResponse
// @Router /workers [get]
func ListWorkers(c *gin.Context) {
	rdb, exists := c.Get("rdb")
	if !exists {
		c.JSON(http.StatusInternalServerError, models.APIErrorResponse{Errors: []string{"Redis client not available"}})
		return
	}

	redisClient, ok := rdb.(*redis.Client)
	if !ok {
		c.JSON(http.StatusInternalServerError, models.APIErrorResponse{Errors: []string{"invalid Redis client type"}})
		return
	}
	ctx := c.Request.Context()

	// Scan for all worker keys matching pattern asynq:workers:*
	var workerKeys []string
	iter := redisClient.Scan(ctx, 0, keyWorkerPattern, 100).Iterator()
	for iter.Next(ctx) {
		key := iter.Val()
		// Skip if it's not a worker data key (e.g., asynq:workers itself)
		if strings.Count(key, ":") >= 2 {
			workerKeys = append(workerKeys, key)
		}
	}
	if err := iter.Err(); err != nil {
		c.JSON(http.StatusInternalServerError, models.APIErrorResponse{Errors: []string{fmt.Sprintf("Failed to scan workers: %v", err)}})
		return
	}

	workers := make([]WorkerInfo, 0, len(workerKeys))
	now := time.Now().Unix()

	for _, workerKey := range workerKeys {
		// Check if it's a HASH (worker data) - skip other types
		keyType, err := redisClient.Type(ctx, workerKey).Result()
		if err != nil || keyType != "hash" {
			continue
		}

		data, err := redisClient.HGetAll(ctx, workerKey).Result()
		if err != nil || len(data) == 0 {
			continue
		}

		// Extract worker ID from key (asynq:workers:{worker_id})
		workerID := strings.TrimPrefix(workerKey, "asynq:workers:")

		worker := WorkerInfo{
			WorkerID:   workerID,
			Status:     data["status"],
			Queues:     data["queues"],
			InstanceID: data["instance_id"],
			Protected:  strings.EqualFold(data["protected"], "true"),
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

		workers = append(workers, worker)
	}

	c.JSON(http.StatusOK, WorkersResponse{
		Workers: workers,
		Count:   len(workers),
	})
}

// GetWorker returns a specific worker's information
// @Summary Get worker details
// @Description Returns details for a specific worker by ID
// @Tags workers
// @Produce json
// @Param worker_id path string true "Worker ID"
// @Success 200 {object} WorkerInfo
// @Failure 404 {object} map[string]string
// @Router /workers/{worker_id} [get]
func GetWorker(c *gin.Context) {
	workerID := c.Param("worker_id")

	rdb, exists := c.Get("rdb")
	if !exists {
		c.JSON(http.StatusInternalServerError, models.APIErrorResponse{Errors: []string{"Redis client not available"}})
		return
	}

	redisClient, ok := rdb.(*redis.Client)
	if !ok {
		c.JSON(http.StatusInternalServerError, models.APIErrorResponse{Errors: []string{"invalid Redis client type"}})
		return
	}
	ctx := c.Request.Context()

	workerKey := "asynq:workers:" + workerID
	data, err := redisClient.HGetAll(ctx, workerKey).Result()
	if err != nil {
		c.JSON(http.StatusInternalServerError, models.APIErrorResponse{Errors: []string{fmt.Sprintf("Failed to get worker: %v", err)}})
		return
	}

	if len(data) == 0 {
		c.JSON(http.StatusNotFound, models.APIErrorResponse{Errors: []string{"Worker not found"}})
		return
	}

	now := time.Now().Unix()
	worker := WorkerInfo{
		WorkerID:   workerID,
		Status:     data["status"],
		Queues:     data["queues"],
		InstanceID: data["instance_id"],
		Protected:  strings.EqualFold(data["protected"], "true"),
	}

	if startedAt, err := strconv.ParseInt(data["started_at"], 10, 64); err == nil {
		worker.StartedAt = startedAt
	}
	if lastBeat, err := strconv.ParseInt(data["last_beat"], 10, 64); err == nil {
		worker.LastBeat = lastBeat
		worker.IsStale = (now - lastBeat) > 30
	}
	if concurrency, err := strconv.Atoi(data["concurrency"]); err == nil {
		worker.Concurrency = concurrency
	}

	c.JSON(http.StatusOK, worker)
}
