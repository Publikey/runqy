package client

import (
	"context"
	"encoding/json"
	"strconv"
	"time"

	queueworker "github.com/Publikey/runqy/queues"
	t "github.com/Publikey/runqy/tasks"
	"github.com/Publikey/runqy/third_party/asynq"
	"github.com/redis/go-redis/v9"
)

// ReverseLookupTTL bounds the lifetime of the asynq:t:<id> reverse-lookup hash
// (task_id -> queue) written on every enqueue. Without it these keys never expire
// and accumulate indefinitely, eventually OOM-ing Redis. It is a generous safety net:
// the worker shortens it to the completed-task TTL once a task finishes, so this only
// caps pathological cases (tasks that never reach a worker: cancelled, queue removed,
// or pending longer than this window — in which case GET /queue/{id} returns 404).
const ReverseLookupTTL = 30 * 24 * time.Hour

// LimitFields stamps the resolved lifecycle values onto the task hash (seconds) so the worker
// enforces them per task. They are written by the enqueue script itself: a worker blocked on
// BRPOP can never pick the task up before they are there.
func LimitFields(limits queueworker.ResolvedLimits, activeTimeout time.Duration) asynq.Option {
	return asynq.TaskFields(map[string]string{
		"ttl_completed":   strconv.FormatInt(int64(limits.TTLCompleted.Seconds()), 10),
		"ttl_archived":    strconv.FormatInt(int64(limits.TTLArchived.Seconds()), 10),
		"pending_timeout": strconv.FormatInt(int64(limits.PendingTimeout.Seconds()), 10),
		"active_timeout":  strconv.FormatInt(int64(activeTimeout.Seconds()), 10),
	})
}

// EnqueueGenericTask enqueues a task with a raw JSON payload to the specified queue
// EnqueueGenericTask enqueues a task. Payload may be json.RawMessage or any typed object
// which will be marshaled to JSON here.
func EnqueueGenericTask(client *asynq.Client, rdb *redis.Client, queue string, timeout int64, payload interface{}, limits queueworker.ResolvedLimits) (*asynq.TaskInfo, error) {
	// Normalize queue name: if no sub-queue specified, append .default
	queue = queueworker.NormalizeQueueName(queue)

	var payloadBytes []byte
	switch p := payload.(type) {
	case json.RawMessage:
		payloadBytes = p
	default:
		b, err := json.Marshal(p)
		if err != nil {
			return nil, err
		}
		payloadBytes = b
	}

	task, err := t.NewGenericTask(queue, payloadBytes)
	if err != nil {
		return nil, err
	}

	// Active execution timeout: a per-request timeout (if provided) overrides the queue default.
	activeTimeout := limits.ActiveTimeout
	if timeout > 0 {
		activeTimeout = time.Duration(timeout) * time.Second
	}

	opts := []asynq.Option{
		asynq.Queue(queue),
		asynq.MaxRetry(limits.MaxRetry),
		asynq.Retention(limits.TTLCompleted), // inspector consistency; worker TTL is authoritative
		LimitFields(limits, activeTimeout),
	}
	if activeTimeout > 0 {
		opts = append(opts, asynq.Timeout(activeTimeout))
	}

	taskInfo, err := client.Enqueue(task, opts...)
	if err != nil {
		return nil, err
	}

	ctx := context.Background()

	// Store queue name in task hash for reverse lookup (GET /queue/{task_id}).
	// Set a TTL so the reverse-lookup key cannot leak forever; the worker realigns
	// it to the completed-task TTL once the task finishes.
	reverseKey := "asynq:t:" + taskInfo.ID
	rdb.HSet(ctx, reverseKey, "queue", queue)
	rdb.Expire(ctx, reverseKey, ReverseLookupTTL)

	// Register queue for asynqmon visibility
	rdb.SAdd(ctx, "asynq:queues", queue)

	return taskInfo, err
}
