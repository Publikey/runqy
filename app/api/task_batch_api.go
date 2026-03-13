package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/Publikey/runqy/models"
	queueworker "github.com/Publikey/runqy/queues"
	t "github.com/Publikey/runqy/tasks"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/jmoiron/sqlx"
	"github.com/Publikey/runqy/third_party/asynq"
	"github.com/redis/go-redis/v9"
)

// BatchTaskRequest represents a batch of tasks to enqueue
type BatchTaskRequest struct {
	Queue   string            `json:"queue" binding:"required"`
	Timeout int64             `json:"timeout"` // Default timeout for all jobs
	Jobs    []json.RawMessage `json:"jobs" binding:"required" swaggertype:"array,object"`
}

// BatchJobWithDeps represents a single job in a batch that may have dependencies
type BatchJobWithDeps struct {
	Data                json.RawMessage `json:"data,omitempty"`
	Ref                 string          `json:"_ref,omitempty"`
	DependsOn           []string        `json:"depends_on,omitempty"`
	DependsOnRef        []string        `json:"depends_on_ref,omitempty"`
	OnParentFailure     string          `json:"on_parent_failure,omitempty"`
	InjectParentResults bool            `json:"inject_parent_results,omitempty"`
}

// BatchTaskResponse contains the results of batch enqueue
type BatchTaskResponse struct {
	Enqueued int      `json:"enqueued"`
	Failed   int      `json:"failed"`
	Waiting  int      `json:"waiting,omitempty"`
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

		// Get timeout (default 30s)
		timeout := req.Timeout
		if timeout == 0 {
			timeout = 30
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

		// First pass: check if any jobs have dependency fields
		hasDeps := false
		var parsedJobs []BatchJobWithDeps
		for _, jobData := range req.Jobs {
			var job BatchJobWithDeps
			if err := json.Unmarshal(jobData, &job); err == nil {
				if len(job.DependsOn) > 0 || len(job.DependsOnRef) > 0 || job.Ref != "" {
					hasDeps = true
				}
			}
			parsedJobs = append(parsedJobs, job)
		}

		// If no deps, use fast path (original logic)
		if !hasDeps {
			for i, jobData := range req.Jobs {
				payload := json.RawMessage(jobData)

				task, err := t.NewGenericTask(queue, payload)
				if err != nil {
					response.Failed++
					response.Errors = append(response.Errors, err.Error())
					continue
				}

				opts := []asynq.Option{
					asynq.Timeout(time.Duration(timeout) * time.Second),
					asynq.Queue(queue),
					asynq.MaxRetry(3),
					asynq.Retention(24 * time.Hour),
				}

				taskInfo, err := asynqClient.Enqueue(task, opts...)
				if err != nil {
					response.Failed++
					response.Errors = append(response.Errors, err.Error())
					continue
				}

				pipe.HSet(ctx, "asynq:t:"+taskInfo.ID, "queue", queue)
				taskEntries = append(taskEntries, taskEntry{taskID: taskInfo.ID, queue: queue})
				response.TaskIDs = append(response.TaskIDs, taskInfo.ID)
				response.Enqueued++

				if (i+1)%100 == 0 {
					if _, err := pipe.Exec(ctx); err != nil {
						response.Errors = append(response.Errors, fmt.Sprintf("pipeline flush error at job %d: %v", i+1, err))
					}
					pipe = rdb.Pipeline()
				}
			}
		} else {
			// Dependency-aware path
			var db *sqlx.DB
			if dbVal, ok := c.Get("db"); ok {
				db, _ = dbVal.(*sqlx.DB)
			}
			if db == nil {
				c.JSON(http.StatusInternalServerError, models.APIErrorResponse{Errors: []string{"database not available for dependency tracking"}})
				return
			}

			// Build ref → taskID map (pre-generate IDs for jobs with _ref)
			refMap := make(map[string]string)
			jobIDs := make([]string, len(parsedJobs))
			for i, job := range parsedJobs {
				id := uuid.New().String()
				jobIDs[i] = id
				if job.Ref != "" {
					refMap[job.Ref] = id
				}
			}

			now := time.Now().Unix()

			for i, job := range parsedJobs {
				taskID := jobIDs[i]

				// Determine payload
				var payload json.RawMessage
				if job.Data != nil {
					payload = job.Data
				} else {
					payload = req.Jobs[i]
				}

				// Resolve depends_on_ref to real IDs
				allDeps := make([]string, 0, len(job.DependsOn)+len(job.DependsOnRef))
				allDeps = append(allDeps, job.DependsOn...)
				for _, ref := range job.DependsOnRef {
					if realID, ok := refMap[ref]; ok {
						allDeps = append(allDeps, realID)
					} else {
						response.Failed++
						response.Errors = append(response.Errors, fmt.Sprintf("job %d: unknown _ref '%s'", i, ref))
						response.TaskIDs = append(response.TaskIDs, "")
						continue
					}
				}

				onParentFailure := job.OnParentFailure
				if onParentFailure == "" {
					onParentFailure = "fail"
				}

				if len(allDeps) == 0 {
					// No deps — enqueue immediately
					task, err := t.NewGenericTask(queue, payload)
					if err != nil {
						response.Failed++
						response.Errors = append(response.Errors, err.Error())
						response.TaskIDs = append(response.TaskIDs, "")
						continue
					}

					opts := []asynq.Option{
						asynq.Timeout(time.Duration(timeout) * time.Second),
						asynq.Queue(queue),
						asynq.MaxRetry(3),
						asynq.Retention(24 * time.Hour),
						asynq.TaskID(taskID),
					}

					_, err = asynqClient.Enqueue(task, opts...)
					if err != nil {
						response.Failed++
						response.Errors = append(response.Errors, err.Error())
						response.TaskIDs = append(response.TaskIDs, "")
						continue
					}

					pipe.HSet(ctx, "asynq:t:"+taskID, "queue", queue)
					taskEntries = append(taskEntries, taskEntry{taskID: taskID, queue: queue})
					response.TaskIDs = append(response.TaskIDs, taskID)
					response.Enqueued++
				} else {
					// Has deps — store as waiting task
					_, err := db.Exec(
						`INSERT INTO waiting_tasks (task_id, queue, payload, on_parent_failure, inject_parent_results, timeout, created_at) VALUES ($1, $2, $3, $4, $5, $6, $7)`,
						taskID, queue, []byte(payload), onParentFailure, job.InjectParentResults, timeout, now,
					)
					if err != nil {
						response.Failed++
						response.Errors = append(response.Errors, fmt.Sprintf("job %d: failed to store waiting task: %v", i, err))
						response.TaskIDs = append(response.TaskIDs, "")
						continue
					}

					// Store queue metadata in Redis
					pipe.HSet(ctx, "asynq:t:"+taskID, "queue", queue)

					for _, depID := range allDeps {
						db.Exec(
							`INSERT INTO task_dependencies (child_id, parent_id, parent_state, created_at) VALUES ($1, $2, 'pending', $3) ON CONFLICT (child_id, parent_id) DO NOTHING`,
							taskID, depID, now,
						)
					}

					response.TaskIDs = append(response.TaskIDs, taskID)
					response.Waiting++
				}
			}
		}

		// Execute remaining pipeline commands
		if len(taskEntries) > 0 || hasDeps {
			pipe.SAdd(ctx, "asynq:queues", queue)
			if _, err := pipe.Exec(ctx); err != nil {
				response.Errors = append(response.Errors, fmt.Sprintf("final pipeline error: %v", err))
			}
		}

		c.JSON(http.StatusOK, response)
	}
}

