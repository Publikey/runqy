package api

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"strings"
	"time"

	_client "github.com/Publikey/runqy/client"
	"github.com/Publikey/runqy/models"
	queueworker "github.com/Publikey/runqy/queues"
	"github.com/Publikey/runqy/utilities"
	"github.com/gin-gonic/gin"
	"github.com/Publikey/runqy/third_party/asynq"
	"github.com/redis/go-redis/v9"
)

// GetTaskStatus godoc
//
//	@Summary		Get the information about the task in the queue and it's response if any.
//	@Description	Retrieve task status instantly, or long-poll with ?wait=true until completed/archived (30s timeout).
//	@Tags			queue
//	@Accept			json
//	@Produce		json
//	@Param			uuid	path		string	true	"The uuid of the task returned from the POST query"
//	@Param			wait	query		string	false	"Set to 'true' to long-poll until completed/archived (default: instant)"
//	@Success		200		{object}	models.ResponseGet
//	@Failure		400		{object}	models.APIErrorResponse
//	@Failure		404		{object}	models.APIErrorResponse
//	@Router			/queue/{uuid} [get]
func GetTaskStatus(c *gin.Context) {
	uuid := c.Param("uuid")

	rdb, ok := c.Get("rdb")
	if !ok {
		c.JSON(http.StatusInternalServerError, models.APIErrorResponse{Errors: []string{"Redis client not available"}})
		return
	}
	redisClient, ok := rdb.(*redis.Client)
	if !ok {
		c.JSON(http.StatusInternalServerError, models.APIErrorResponse{Errors: []string{"invalid Redis client type"}})
		return
	}
	redisOptVal, ok := c.Get("redisOpt")
	if !ok {
		c.JSON(http.StatusInternalServerError, models.APIErrorResponse{Errors: []string{"Redis options not available"}})
		return
	}
	redisOpt, ok := redisOptVal.(asynq.RedisClientOpt)
	if !ok {
		c.JSON(http.StatusInternalServerError, models.APIErrorResponse{Errors: []string{"invalid Redis options type"}})
		return
	}

	// Look up queue from task hash: asynq:t:{task_id}
	taskKey := fmt.Sprintf("asynq:t:%s", uuid)
	queue, err := redisClient.HGet(c, taskKey, "queue").Result()
	if err == redis.Nil {
		c.JSON(http.StatusNotFound, models.APIErrorResponse{Errors: []string{"task not found"}})
		return
	}
	if err != nil {
		c.JSON(http.StatusInternalServerError, models.APIErrorResponse{Errors: []string{err.Error()}})
		return
	}

	inspector := asynq.NewInspector(redisOpt)
	defer inspector.Close()

	var resp *asynq.TaskInfo

	if c.Query("wait") == "true" {
		// Long poll: wait up to 30s for completed/archived
		resp, err = waitForResult(c.Request.Context(), inspector, queue, uuid)
		if err != nil {
			c.JSON(http.StatusRequestTimeout, models.APIErrorResponse{Errors: []string{err.Error()}})
			return
		}
	} else {
		// Instant: return current state immediately
		resp, err = inspector.GetTaskInfo(queue, uuid)
		if err != nil {
			c.JSON(http.StatusInternalServerError, models.APIErrorResponse{Errors: []string{err.Error()}})
			return
		}
	}

	taskDoc := models.GetTaskInfoDoc{
		ID:            resp.ID,
		Type:          resp.Type,
		Payload:       utilities.DecodeBase64OrReturnRaw(resp.Payload),
		State:         resp.State.String(),
		Queue:         resp.Queue,
		MaxRetry:      resp.MaxRetry,
		Retried:       resp.Retried,
		LastErr:       resp.LastErr,
		LastFailedAt:  resp.LastFailedAt,
		Deadline:      resp.Deadline,
		Group:         resp.Group,
		NextProcessAt: resp.NextProcessAt,
		IsOrphaned:    resp.IsOrphaned,
		CompletedAt:   resp.CompletedAt,
		Result:        utilities.DecodeBase64OrReturnRaw(resp.Result),
	}
	response := models.ResponseGet{
		Info: taskDoc,
	}
	c.JSON(http.StatusOK, response)
}

// AddTask godoc
//
//	@Summary		Send a new task to the queuer
//	@Description	Send a generic task request with queue, timeout, and a flexible JSON payload.
//	@Tags			queue
//	@Accept			json
//	@Produce		json
//	@Param			task	body		models.GenericTask	true	"Task with queue, timeout, and payload"
//	@Success		200		{object}	models.GenericResponsePost
//	@Failure		400		{object}	models.APIErrorResponse
//	@Router			/queue/add [post]
//
// AddTask returns a handler that validates the incoming task `data` against
// the queue worker YAML schemas found in `qwConfigDir` before enqueuing.
func AddTask(qwConfigDir string, qwStore *queueworker.Store) gin.HandlerFunc {
	return func(c *gin.Context) {
        // Parse as raw JSON to support both flat and nested formats
        var rawBody map[string]json.RawMessage
        if err := c.ShouldBindBodyWithJSON(&rawBody); err != nil {
            c.JSON(http.StatusBadRequest, models.APIErrorResponse{Errors: []string{err.Error()}})
            return
        }

        // Extract queue (required)
        var queue string
        if queueRaw, ok := rawBody["queue"]; ok {
            if err := json.Unmarshal(queueRaw, &queue); err != nil {
                c.JSON(http.StatusBadRequest, models.APIErrorResponse{Errors: []string{"invalid queue field: " + err.Error()}})
                return
            }
        }
        if queue == "" {
            c.JSON(http.StatusBadRequest, models.APIErrorResponse{Errors: []string{"queue is required"}})
            return
        }

        // Normalize queue name: if no sub-queue specified, append .default
        queue = queueworker.NormalizeQueueName(queue)

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

        // Extract timeout (required)
        var timeout int64
        if timeoutRaw, ok := rawBody["timeout"]; ok {
            if err := json.Unmarshal(timeoutRaw, &timeout); err != nil {
                c.JSON(http.StatusBadRequest, models.APIErrorResponse{Errors: []string{"invalid timeout field: " + err.Error()}})
                return
            }
        }

        // Determine the data payload
        var dataBytes json.RawMessage
        if dataRaw, ok := rawBody["data"]; ok {
            // Nested format: use "data" field directly
            dataBytes = dataRaw
        } else {
            // Flat format: collect all fields except queue and timeout
            flatData := make(map[string]json.RawMessage)
            for k, v := range rawBody {
                if k != "queue" && k != "timeout" {
                    flatData[k] = v
                }
            }
            var err error
            dataBytes, err = json.Marshal(flatData)
            if err != nil {
                c.JSON(http.StatusBadRequest, models.APIErrorResponse{Errors: []string{"failed to build data payload: " + err.Error()}})
                return
            }
        }

        // Build query struct for compatibility with rest of the code
        query := models.GenericTask{
            Queue:   queue,
            Timeout: timeout,
            Data:    dataBytes,
        }

        // Prepare payloadToSend (TypedPayload by default)
        var payloadToSend interface{}

		// Validate payload against YAML schema for this queue if available
		yamls, err := queueworker.LoadAllCached(qwConfigDir)
		if err == nil && len(yamls) > 0 {
			var matched *queueworker.QueueYAML
			// Try to find the queue by matching runtime config names
			for _, y := range yamls {
				for baseName, qcfg := range y.Queues {
					configs := qcfg.ToQueueConfigs(baseName)
					for _, cfg := range configs {
						if cfg.Name == query.Queue {
							matched = &qcfg
							break
						}
					}
					if matched != nil {
						break
					}
				}
				if matched != nil {
					break
				}
			}

			// If not found, try trimming a trailing .default
			if matched == nil && strings.HasSuffix(query.Queue, ".default") {
				base := strings.TrimSuffix(query.Queue, ".default")
				for _, y := range yamls {
					if qcfg, ok := y.Queues[base]; ok {
						matched = &qcfg
						break
					}
				}
			}

			if matched != nil && len(matched.Input) > 0 {
				var dataMap map[string]interface{}
				if err := json.Unmarshal(query.Data, &dataMap); err != nil {
					c.JSON(http.StatusBadRequest, models.APIErrorResponse{Errors: []string{"invalid data payload: " + err.Error()}})
					return
				}

				filtered, err := validateFields(dataMap, matched.Input)
				if err != nil {
					c.JSON(http.StatusBadRequest, models.APIErrorResponse{Errors: []string{err.Error()}})
					return
				}

				payloadToSend = models.TypedPayload(filtered)
			}
			// if matched == nil, fallthrough and parse into TypedPayload below
		}

		// If payloadToSend is still nil, create a TypedPayload from the raw data
		if payloadToSend == nil {
			var dataMap map[string]interface{}
			if err := json.Unmarshal(query.Data, &dataMap); err != nil {
				// If unmarshalling fails, fallback to sending raw bytes
				payloadToSend = json.RawMessage(query.Data)
			} else {
				payloadToSend = models.TypedPayload(dataMap)
			}
		}

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

		// Resolve task lifecycle limits from the parent queue config (server defaults ⊕ override).
		parentQueue, _, _ := queueworker.ParseQueueName(query.Queue)
		limits := queueworker.DefaultLimits()
		if pq, gerr := qwStore.GetQueue(c.Request.Context(), parentQueue); gerr == nil && pq != nil {
			limits = queueworker.ResolveQueueLimits(pq)
		}

		info, err := _client.EnqueueGenericTask(asynqClient, rdb, query.Queue, query.Timeout, payloadToSend, limits)
		if err != nil {
			c.JSON(http.StatusBadRequest, models.APIErrorResponse{Errors: []string{err.Error()}})
			return
		}
		// Manual mapping from *asynq.TaskInfo to TaskInfoDoc
		taskDoc := models.AddTaskInfoDoc{
			ID:            info.ID,
			Type:          info.Type,
			Payload:       info.Payload,
			State:         info.State.String(),
			Queue:         info.Queue,
			MaxRetry:      info.MaxRetry,
			Retried:       info.Retried,
			LastErr:       info.LastErr,
			LastFailedAt:  info.LastFailedAt,
			Deadline:      info.Deadline,
			Group:         info.Group,
			NextProcessAt: info.NextProcessAt,
			IsOrphaned:    info.IsOrphaned,
			CompletedAt:   info.CompletedAt,
			Result:        info.Result,
		}
		response := models.GenericResponsePost{
			Info: taskDoc,
			Data: query.Data,
		}
		c.JSON(http.StatusOK, response)
	}

}

func waitForResult(ctx context.Context, i *asynq.Inspector, queue, taskID string) (*asynq.TaskInfo, error) {
	t := time.NewTicker(time.Second)
	defer t.Stop()

	timeout := time.After(30 * time.Second)

	for {
		select {
		case <-t.C:
			taskInfo, err := i.GetTaskInfo(queue, taskID)
			if err != nil {
				return nil, err
			}
			if taskInfo.State == asynq.TaskStateCompleted || taskInfo.State == asynq.TaskStateArchived {
				return taskInfo, nil
			}
			// Continue polling for non-terminal states
		case <-timeout:
			return nil, fmt.Errorf("timeout waiting for task result")
		case <-ctx.Done():
			return nil, fmt.Errorf("context closed")
		}
	}
}

// checkAllowedType checks if the provided value matches any of the allowed types
func checkAllowedType(v interface{}, allowed []string) bool {
	switch val := v.(type) {
	case string:
		return contains(allowed, "string")
	case bool:
		return contains(allowed, "bool")
	case float64:
		// JSON numbers are float64; determine if integer-valued
		if math.Trunc(val) == val {
			return contains(allowed, "int") || contains(allowed, "float")
		}
		return contains(allowed, "float")
	case []interface{}:
		return contains(allowed, "array")
	case map[string]interface{}:
		return contains(allowed, "object")
	default:
		return false
	}
}

// describeType returns a human-readable type name for a JSON value
func describeType(v interface{}) string {
	switch v.(type) {
	case string:
		return "string"
	case bool:
		return "bool"
	case float64:
		return "number"
	case []interface{}:
		return "array"
	case map[string]interface{}:
		return "object"
	default:
		return fmt.Sprintf("%T", v)
	}
}

func contains(arr []string, s string) bool {
	for _, v := range arr {
		if v == s {
			return true
		}
	}
	return false
}

// validateFields validates a data map against a list of FieldSchema definitions.
// It passes through all fields from the input, then validates and applies defaults
// for schema-defined fields.
func validateFields(dataMap map[string]interface{}, fields []queueworker.FieldSchema) (map[string]interface{}, error) {
	// Start with all fields (pass-through)
	filtered := make(map[string]interface{})
	for k, v := range dataMap {
		filtered[k] = v
	}

	for _, field := range fields {
		val, exists := filtered[field.Name]

		if !exists {
			if field.IsRequired() {
				return nil, fmt.Errorf("%s is required", field.Name)
			}
			// Optional: apply default if defined
			if field.Default != nil {
				defVal := field.Default
				// Handle float64 -> int64 conversion for defaults (JSON round-trip)
				if contains(field.Type, "int") {
					if f, ok := defVal.(float64); ok {
						defVal = int64(f)
					}
				}
				filtered[field.Name] = defVal
			}
			continue
		}

		// Validate type
		if !checkAllowedType(val, field.Type) {
			return nil, fmt.Errorf("field '%s' has type %s, expected one of: %s", field.Name, describeType(val), strings.Join(field.Type, ", "))
		}

		// Convert float64 -> int64 for int-typed fields
		if contains(field.Type, "int") {
			if f, ok := val.(float64); ok {
				filtered[field.Name] = int64(f)
			}
		}
	}

	return filtered, nil
}
