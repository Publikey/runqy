package api

import (
	"fmt"
	"log"
	"net/http"
	"strconv"

	"github.com/Publikey/runqy/models"
	queueworker "github.com/Publikey/runqy/queues"
	"github.com/gin-gonic/gin"
	"github.com/Publikey/runqy/third_party/asynq"
	"github.com/redis/go-redis/v9"
)

// QueueInfoResponse represents queue information for CLI
type QueueInfoResponse struct {
	Queue       string `json:"queue"`
	Pending     int    `json:"pending"`
	Active      int    `json:"active"`
	Scheduled   int    `json:"scheduled"`
	Retry       int    `json:"retry"`
	Archived    int    `json:"archived"`
	Completed   int    `json:"completed"`
	Paused      bool   `json:"paused"`
	MemoryUsage int64  `json:"memory_usage"`
}

// QueueListResponse is the response for listing queues
type QueueListResponse struct {
	Queues   []QueueInfoResponse `json:"queues"`
	Warnings []string            `json:"warnings,omitempty"`
}

// TaskInfoResponse represents task information for CLI
type TaskInfoResponse struct {
	ID            string `json:"id"`
	Type          string `json:"type"`
	Queue         string `json:"queue"`
	State         string `json:"state"`
	Payload       string `json:"payload"`
	MaxRetry      int    `json:"max_retry"`
	Retried       int    `json:"retried"`
	LastErr       string `json:"last_err,omitempty"`
	Timeout       string `json:"timeout"`
	Deadline      string `json:"deadline,omitempty"`
	NextProcessAt string `json:"next_process_at,omitempty"`
	CompletedAt   string `json:"completed_at,omitempty"`
	Result        string `json:"result,omitempty"`
}

// TaskListResponse is the response for listing tasks
type TaskListResponse struct {
	Tasks []TaskInfoResponse `json:"tasks"`
}

// SetupCLIAPI sets up the CLI-specific API endpoints
func SetupCLIAPI(r *gin.Engine, inspector *asynq.Inspector, store *queueworker.Store, apiKey string) {
	api := r.Group("/api")
	api.Use(Authorize(apiKey))

	// Queue endpoints
	api.GET("/queues", listQueuesHandler(inspector, store))
	api.GET("/queues/:queue", getQueueHandler(inspector))
	api.POST("/queues/:queue/pause", pauseQueueHandler(inspector))
	api.POST("/queues/:queue/unpause", unpauseQueueHandler(inspector))
	api.DELETE("/queues/:queue", deleteQueueHandler(inspector, store))

	// Task endpoints
	api.GET("/queues/:queue/tasks", listTasksHandler(inspector))
	api.GET("/queues/:queue/tasks/:task_id", getTaskHandler(inspector))
	api.DELETE("/queues/:queue/tasks/:task_id", deleteTaskHandler(inspector))
	api.POST("/tasks/:task_id/cancel", cancelTaskHandler(inspector))

	// Additional endpoints for UI compatibility
	api.GET("/queue_stats", queueStatsHandler(inspector))
	api.GET("/workers", workersHandler(store))
	api.GET("/servers", serversHandler(inspector))
}

func listQueuesHandler(inspector *asynq.Inspector, store *queueworker.Store) gin.HandlerFunc {
	return func(c *gin.Context) {
		// Sync configs from PostgreSQL to asynq before listing
		if err := store.SyncConfigsToAsynq(c.Request.Context()); err != nil {
			log.Printf("Warning: failed to sync configs to asynq: %v", err)
		}

		queues, err := inspector.Queues()
		if err != nil {
			c.JSON(http.StatusInternalServerError, models.APIErrorResponse{Errors: []string{err.Error()}})
			return
		}

		var result []QueueInfoResponse
		var warnings []string
		for _, queueName := range queues {
			info, err := inspector.GetQueueInfo(queueName)
			if err != nil {
				warnings = append(warnings, fmt.Sprintf("failed to get info for queue '%s': %v", queueName, err))
				continue
			}

			result = append(result, QueueInfoResponse{
				Queue:       info.Queue,
				Pending:     info.Pending,
				Active:      info.Active,
				Scheduled:   info.Scheduled,
				Retry:       info.Retry,
				Archived:    info.Archived,
				Completed:   info.Completed,
				Paused:      info.Paused,
				MemoryUsage: info.MemoryUsage,
			})
		}

		c.JSON(http.StatusOK, QueueListResponse{Queues: result, Warnings: warnings})
	}
}

func getQueueHandler(inspector *asynq.Inspector) gin.HandlerFunc {
	return func(c *gin.Context) {
		queueName := c.Param("queue")

		info, err := inspector.GetQueueInfo(queueName)
		if err != nil {
			c.JSON(http.StatusNotFound, models.APIErrorResponse{Errors: []string{err.Error()}})
			return
		}

		c.JSON(http.StatusOK, QueueInfoResponse{
			Queue:       info.Queue,
			Pending:     info.Pending,
			Active:      info.Active,
			Scheduled:   info.Scheduled,
			Retry:       info.Retry,
			Archived:    info.Archived,
			Completed:   info.Completed,
			Paused:      info.Paused,
			MemoryUsage: info.MemoryUsage,
		})
	}
}

func pauseQueueHandler(inspector *asynq.Inspector) gin.HandlerFunc {
	return func(c *gin.Context) {
		queueName := c.Param("queue")

		if err := inspector.PauseQueue(queueName); err != nil {
			c.JSON(http.StatusInternalServerError, models.APIErrorResponse{Errors: []string{err.Error()}})
			return
		}

		c.JSON(http.StatusOK, gin.H{"message": "queue paused", "queue": queueName})
	}
}

func unpauseQueueHandler(inspector *asynq.Inspector) gin.HandlerFunc {
	return func(c *gin.Context) {
		queueName := c.Param("queue")

		if err := inspector.UnpauseQueue(queueName); err != nil {
			c.JSON(http.StatusInternalServerError, models.APIErrorResponse{Errors: []string{err.Error()}})
			return
		}

		c.JSON(http.StatusOK, gin.H{"message": "queue unpaused", "queue": queueName})
	}
}

func listTasksHandler(inspector *asynq.Inspector) gin.HandlerFunc {
	return func(c *gin.Context) {
		queueName := c.Param("queue")
		state := c.DefaultQuery("state", "pending")
		limitStr := c.DefaultQuery("limit", "10")

		limit, err := strconv.Atoi(limitStr)
		if err != nil {
			limit = 10
		}

		var tasks []TaskInfoResponse

		switch state {
		case "pending":
			taskList, err := inspector.ListPendingTasks(queueName, asynq.PageSize(limit))
			if err != nil {
				c.JSON(http.StatusInternalServerError, models.APIErrorResponse{Errors: []string{err.Error()}})
				return
			}
			for _, t := range taskList {
				tasks = append(tasks, TaskInfoResponse{
					ID:       t.ID,
					Type:     t.Type,
					Queue:    t.Queue,
					State:    t.State.String(),
					Payload:  string(t.Payload),
					MaxRetry: t.MaxRetry,
					Retried:  t.Retried,
				})
			}

		case "active":
			taskList, err := inspector.ListActiveTasks(queueName, asynq.PageSize(limit))
			if err != nil {
				c.JSON(http.StatusInternalServerError, models.APIErrorResponse{Errors: []string{err.Error()}})
				return
			}
			for _, t := range taskList {
				tasks = append(tasks, TaskInfoResponse{
					ID:       t.ID,
					Type:     t.Type,
					Queue:    t.Queue,
					State:    t.State.String(),
					Payload:  string(t.Payload),
					MaxRetry: t.MaxRetry,
					Retried:  t.Retried,
				})
			}

		case "scheduled":
			taskList, err := inspector.ListScheduledTasks(queueName, asynq.PageSize(limit))
			if err != nil {
				c.JSON(http.StatusInternalServerError, models.APIErrorResponse{Errors: []string{err.Error()}})
				return
			}
			for _, t := range taskList {
				tasks = append(tasks, TaskInfoResponse{
					ID:            t.ID,
					Type:          t.Type,
					Queue:         t.Queue,
					State:         t.State.String(),
					Payload:       string(t.Payload),
					MaxRetry:      t.MaxRetry,
					Retried:       t.Retried,
					NextProcessAt: t.NextProcessAt.Format("2006-01-02 15:04:05"),
				})
			}

		case "retry":
			taskList, err := inspector.ListRetryTasks(queueName, asynq.PageSize(limit))
			if err != nil {
				c.JSON(http.StatusInternalServerError, models.APIErrorResponse{Errors: []string{err.Error()}})
				return
			}
			for _, t := range taskList {
				tasks = append(tasks, TaskInfoResponse{
					ID:            t.ID,
					Type:          t.Type,
					Queue:         t.Queue,
					State:         t.State.String(),
					Payload:       string(t.Payload),
					MaxRetry:      t.MaxRetry,
					Retried:       t.Retried,
					LastErr:       t.LastErr,
					NextProcessAt: t.NextProcessAt.Format("2006-01-02 15:04:05"),
				})
			}

		case "archived":
			taskList, err := inspector.ListArchivedTasks(queueName, asynq.PageSize(limit))
			if err != nil {
				c.JSON(http.StatusInternalServerError, models.APIErrorResponse{Errors: []string{err.Error()}})
				return
			}
			for _, t := range taskList {
				tasks = append(tasks, TaskInfoResponse{
					ID:       t.ID,
					Type:     t.Type,
					Queue:    t.Queue,
					State:    t.State.String(),
					Payload:  string(t.Payload),
					MaxRetry: t.MaxRetry,
					Retried:  t.Retried,
					LastErr:  t.LastErr,
				})
			}

		case "completed":
			taskList, err := inspector.ListCompletedTasks(queueName, asynq.PageSize(limit))
			if err != nil {
				c.JSON(http.StatusInternalServerError, models.APIErrorResponse{Errors: []string{err.Error()}})
				return
			}
			for _, t := range taskList {
				tasks = append(tasks, TaskInfoResponse{
					ID:          t.ID,
					Type:        t.Type,
					Queue:       t.Queue,
					State:       t.State.String(),
					Payload:     string(t.Payload),
					MaxRetry:    t.MaxRetry,
					Retried:     t.Retried,
					CompletedAt: t.CompletedAt.Format("2006-01-02 15:04:05"),
					Result:      string(t.Result),
				})
			}

		default:
			c.JSON(http.StatusBadRequest, models.APIErrorResponse{Errors: []string{"invalid state: " + state}})
			return
		}

		c.JSON(http.StatusOK, TaskListResponse{Tasks: tasks})
	}
}

func getTaskHandler(inspector *asynq.Inspector) gin.HandlerFunc {
	return func(c *gin.Context) {
		queueName := c.Param("queue")
		taskID := c.Param("task_id")

		info, err := inspector.GetTaskInfo(queueName, taskID)
		if err != nil {
			c.JSON(http.StatusNotFound, models.APIErrorResponse{Errors: []string{err.Error()}})
			return
		}

		resp := TaskInfoResponse{
			ID:       info.ID,
			Type:     info.Type,
			Queue:    info.Queue,
			State:    info.State.String(),
			Payload:  string(info.Payload),
			MaxRetry: info.MaxRetry,
			Retried:  info.Retried,
			LastErr:  info.LastErr,
			Timeout:  info.Timeout.String(),
		}

		if !info.Deadline.IsZero() {
			resp.Deadline = info.Deadline.Format("2006-01-02 15:04:05")
		}
		if !info.NextProcessAt.IsZero() {
			resp.NextProcessAt = info.NextProcessAt.Format("2006-01-02 15:04:05")
		}
		if !info.CompletedAt.IsZero() {
			resp.CompletedAt = info.CompletedAt.Format("2006-01-02 15:04:05")
		}
		if len(info.Result) > 0 {
			resp.Result = string(info.Result)
		}

		c.JSON(http.StatusOK, resp)
	}
}

func deleteTaskHandler(inspector *asynq.Inspector) gin.HandlerFunc {
	return func(c *gin.Context) {
		queueName := c.Param("queue")
		taskID := c.Param("task_id")

		if err := inspector.DeleteTask(queueName, taskID); err != nil {
			c.JSON(http.StatusInternalServerError, models.APIErrorResponse{Errors: []string{err.Error()}})
			return
		}

		// Also remove the reverse-lookup key (asynq:t:<id>): it is not queue-scoped,
		// so inspector.DeleteTask (which only touches asynq:{queue}:t:<id>) leaves it behind.
		if rdb, ok := c.Get("rdb"); ok {
			if redisClient, ok := rdb.(*redis.Client); ok {
				redisClient.Del(c, "asynq:t:"+taskID)
			}
		}

		c.JSON(http.StatusOK, gin.H{"message": "task deleted", "task_id": taskID})
	}
}

func cancelTaskHandler(inspector *asynq.Inspector) gin.HandlerFunc {
	return func(c *gin.Context) {
		taskID := c.Param("task_id")

		if err := inspector.CancelProcessing(taskID); err != nil {
			c.JSON(http.StatusInternalServerError, models.APIErrorResponse{Errors: []string{err.Error()}})
			return
		}

		c.JSON(http.StatusOK, gin.H{"message": "task cancelled", "task_id": taskID})
	}
}

func deleteQueueHandler(inspector *asynq.Inspector, store *queueworker.Store) gin.HandlerFunc {
	return func(c *gin.Context) {
		queueName := c.Param("queue")
		force := c.Query("force") == "true"

		// Delete from Redis (asynq queue data)
		if err := inspector.DeleteQueue(queueName, force); err != nil {
			// Ignore "queue not found" in Redis - it might only exist in DB
			if err != asynq.ErrQueueNotFound {
				if err == asynq.ErrQueueNotEmpty {
					c.JSON(http.StatusBadRequest, models.APIErrorResponse{Errors: []string{"queue is not empty - use force=true to delete anyway"}})
					return
				}
				c.JSON(http.StatusInternalServerError, models.APIErrorResponse{Errors: []string{err.Error()}})
				return
			}
		}

		// Delete from database (queue configuration)
		if store != nil {
			if err := store.Delete(c.Request.Context(), queueName); err != nil {
				// Log but don't fail - Redis deletion already succeeded
				log.Printf("Warning: failed to delete queue config from database: %v", err)
			}
		}

		c.JSON(http.StatusOK, gin.H{"message": "queue deleted", "queue": queueName})
	}
}

func queueStatsHandler(inspector *asynq.Inspector) gin.HandlerFunc {
	return func(c *gin.Context) {
		// Return empty stats - use /monitoring/api/queue_stats for full data
		c.JSON(http.StatusOK, gin.H{"stats": []interface{}{}})
	}
}

func workersHandler(_ *queueworker.Store) gin.HandlerFunc {
	return func(c *gin.Context) {
		// Return empty workers - use /monitoring/api/workers for full data
		c.JSON(http.StatusOK, gin.H{"workers": []interface{}{}})
	}
}

func serversHandler(_ *asynq.Inspector) gin.HandlerFunc {
	return func(c *gin.Context) {
		// Return empty servers - use /monitoring/api/servers for full data
		c.JSON(http.StatusOK, gin.H{"servers": []interface{}{}})
	}
}
