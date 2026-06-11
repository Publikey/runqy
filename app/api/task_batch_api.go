package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/Publikey/runqy/client"
	"github.com/Publikey/runqy/models"
	queueworker "github.com/Publikey/runqy/queues"
	t "github.com/Publikey/runqy/tasks"
	"github.com/gin-gonic/gin"
	"github.com/Publikey/runqy/third_party/asynq"
	"github.com/redis/go-redis/v9"
)

// BatchTaskRequest represents a batch of tasks to enqueue
type BatchTaskRequest struct {
	Queue   string            `json:"queue" binding:"required"`
	Timeout int64             `json:"timeout"` // Default timeout for all jobs
	Jobs    []json.RawMessage `json:"jobs" binding:"required" swaggertype:"array,object"`
}

// BatchTaskResponse contains the results of batch enqueue
type BatchTaskResponse struct {
	Enqueued int      `json:"enqueued"`
	Failed   int      `json:"failed"`
	TaskIDs  []string `json:"task_ids"`
	Errors   []string `json:"errors,omitempty"`
}

// AddTaskBatch godoc
//
//	@Summary		Send multiple tasks to the queue in a single request
//	@Description	Batch enqueue for high-throughput job submission. Uses Redis pipelining for optimal performance.
//	@Tags			queue
//	@Accept			json
//	@Produce		json
//	@Param			batch	body		BatchTaskRequest	true	"Batch of tasks with queue and jobs array"
//	@Success		200		{object}	BatchTaskResponse
//	@Failure		400		{object}	models.APIErrorResponse
//	@Router			/queue/add-batch [post]
//
// AddTaskBatch returns a handler that enqueues multiple tasks using Redis pipelining
func AddTaskBatch(qwConfigDir string, qwStore *queueworker.Store) gin.HandlerFunc {
	return func(c *gin.Context) {
		var req BatchTaskRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, models.APIErrorResponse{Errors: []string{err.Error()}})
			return
		}

		if len(req.Jobs) == 0 {
			c.JSON(http.StatusBadRequest, models.APIErrorResponse{Errors: []string{"jobs array cannot be empty"}})
			return
		}

		// Normalize queue name
		queue := queueworker.NormalizeQueueName(req.Queue)

		// Validate queue exists in database
		exists, err := qwStore.Exists(c.Request.Context(), queue)
		if err != nil {
			c.JSON(http.StatusInternalServerError, models.APIErrorResponse{Errors: []string{"failed to check queue: " + err.Error()}})
			return
		}
		if !exists {
			c.JSON(http.StatusNotFound, models.APIErrorResponse{Errors: []string{"queue not found: " + queue}})
			return
		}

		// Resolve task lifecycle limits from the parent queue config (server defaults ⊕ override).
		parentQueue, _, _ := queueworker.ParseQueueName(queue)
		limits := queueworker.DefaultLimits()
		if pq, gerr := qwStore.GetQueue(c.Request.Context(), parentQueue); gerr == nil && pq != nil {
			limits = queueworker.ResolveQueueLimits(pq)
		}
		// Active execution timeout the worker enforces: an explicit per-request timeout overrides
		// the queue default. Note: unlike the old code we do NOT default this to 30s, so existing
		// batches without an explicit timeout keep their (disabled-by-default) behaviour.
		activeTimeout := limits.ActiveTimeout
		if req.Timeout > 0 {
			activeTimeout = time.Duration(req.Timeout) * time.Second
		}

		// Get asynq client and redis client (safe assertions)
		asynqClientVal, ok := c.Get("client")
		if !ok {
			c.JSON(http.StatusInternalServerError, models.APIErrorResponse{Errors: []string{"asynq client not available"}})
			return
		}
		asynqClient, ok := asynqClientVal.(*asynq.Client)
		if !ok {
			c.JSON(http.StatusInternalServerError, models.APIErrorResponse{Errors: []string{"invalid asynq client type"}})
			return
		}
		rdbVal, ok := c.Get("rdb")
		if !ok {
			c.JSON(http.StatusInternalServerError, models.APIErrorResponse{Errors: []string{"Redis client not available"}})
			return
		}
		rdb, ok := rdbVal.(*redis.Client)
		if !ok {
			c.JSON(http.StatusInternalServerError, models.APIErrorResponse{Errors: []string{"invalid Redis client type"}})
			return
		}

		// Prepare batch
		response := BatchTaskResponse{
			TaskIDs: make([]string, 0, len(req.Jobs)),
			Errors:  make([]string, 0),
		}

		// Use Redis pipeline for batch operations
		ctx := c.Request.Context()
		pipe := rdb.Pipeline()

		// Collect task infos for the response
		type taskEntry struct {
			taskID string
			queue  string
		}
		var taskEntries []taskEntry

		for i, jobData := range req.Jobs {
			payload := json.RawMessage(jobData)

			// Create task
			task, err := t.NewGenericTask(queue, payload)
			if err != nil {
				response.Failed++
				response.Errors = append(response.Errors, err.Error())
				continue
			}

			// Enqueue task
			opts := []asynq.Option{
				asynq.Queue(queue),
				asynq.MaxRetry(limits.MaxRetry),
				asynq.Retention(limits.TTLCompleted), // inspector consistency; worker TTL is authoritative
			}
			if activeTimeout > 0 {
				opts = append(opts, asynq.Timeout(activeTimeout))
			}

			taskInfo, err := asynqClient.Enqueue(task, opts...)
			if err != nil {
				response.Failed++
				response.Errors = append(response.Errors, err.Error())
				continue
			}

			// Stamp resolved lifecycle values + reverse-lookup in the pipeline (non-blocking).
			taskKey := "asynq:{" + queue + "}:t:" + taskInfo.ID
			pipe.HSet(ctx, taskKey,
				"ttl_completed", int64(limits.TTLCompleted.Seconds()),
				"ttl_archived", int64(limits.TTLArchived.Seconds()),
				"pending_timeout", int64(limits.PendingTimeout.Seconds()),
				"active_timeout", int64(activeTimeout.Seconds()),
			)
			reverseKey := "asynq:t:" + taskInfo.ID
			pipe.HSet(ctx, reverseKey, "queue", queue)
			pipe.Expire(ctx, reverseKey, client.ReverseLookupTTL)
			taskEntries = append(taskEntries, taskEntry{taskID: taskInfo.ID, queue: queue})

			response.TaskIDs = append(response.TaskIDs, taskInfo.ID)
			response.Enqueued++

			// Flush pipeline every 100 jobs to prevent memory buildup
			if (i+1)%100 == 0 {
				if _, err := pipe.Exec(ctx); err != nil {
					response.Errors = append(response.Errors, fmt.Sprintf("pipeline flush error at job %d: %v", i+1, err))
				}
				pipe = rdb.Pipeline()
			}
		}

		// Execute remaining pipeline commands
		if len(taskEntries) > 0 {
			pipe.SAdd(ctx, "asynq:queues", queue)
			if _, err := pipe.Exec(ctx); err != nil {
				response.Errors = append(response.Errors, fmt.Sprintf("final pipeline error: %v", err))
			}
		}

		c.JSON(http.StatusOK, response)
	}
}

