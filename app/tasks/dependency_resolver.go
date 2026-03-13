package tasks

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"time"

	"github.com/jmoiron/sqlx"
	"github.com/Publikey/runqy/models"
	"github.com/Publikey/runqy/third_party/asynq"
	"github.com/redis/go-redis/v9"
)

// ResolveDependencies processes a completed parent task and resolves dependencies.
// It updates dependency states, checks for cascade failures, and enqueues children
// whose dependencies are all met.
func ResolveDependencies(db *sqlx.DB, asynqClient *asynq.Client, redisClient *redis.Client, completedTaskID string, completedState string) error {
	ctx := context.Background()

	// Update all dependency rows for this parent
	_, err := db.Exec(
		`UPDATE task_dependencies SET parent_state = $1 WHERE parent_id = $2 AND parent_state = 'pending'`,
		completedState, completedTaskID,
	)
	if err != nil {
		return fmt.Errorf("failed to update parent state: %w", err)
	}

	// Find all children that depend on this parent
	var childIDs []string
	err = db.Select(&childIDs, `SELECT DISTINCT child_id FROM task_dependencies WHERE parent_id = $1`, completedTaskID)
	if err != nil {
		return fmt.Errorf("failed to find children: %w", err)
	}

	for _, childID := range childIDs {
		if err := resolveChild(ctx, db, asynqClient, redisClient, childID); err != nil {
			log.Printf("[DEPS] Error resolving child %s: %v", childID, err)
		}
	}

	return nil
}

func resolveChild(ctx context.Context, db *sqlx.DB, asynqClient *asynq.Client, redisClient *redis.Client, childID string) error {
	// Fetch the waiting task
	var wt models.WaitingTask
	err := db.Get(&wt, `SELECT * FROM waiting_tasks WHERE task_id = $1`, childID)
	if err != nil {
		// Child may have already been processed or cascade-failed
		return nil
	}

	// Check if any parent has failed and child has on_parent_failure="fail"
	if wt.OnParentFailure == "fail" {
		var failedCount int
		err := db.Get(&failedCount,
			`SELECT COUNT(*) FROM task_dependencies WHERE child_id = $1 AND parent_state IN ('archived', 'failed')`,
			childID,
		)
		if err != nil {
			return fmt.Errorf("failed to check for failed parents: %w", err)
		}
		if failedCount > 0 {
			return cascadeFailChild(ctx, db, asynqClient, redisClient, childID)
		}
	}

	// Check if ALL parent deps are resolved (no pending ones remain)
	var pendingCount int
	err = db.Get(&pendingCount,
		`SELECT COUNT(*) FROM task_dependencies WHERE child_id = $1 AND parent_state = 'pending'`,
		childID,
	)
	if err != nil {
		return fmt.Errorf("failed to check pending deps: %w", err)
	}

	if pendingCount > 0 {
		return nil // Still waiting on parents
	}

	// All deps resolved — enqueue the child
	return enqueueWaitingTask(ctx, db, asynqClient, redisClient, wt)
}

func cascadeFailChild(ctx context.Context, db *sqlx.DB, asynqClient *asynq.Client, redisClient *redis.Client, childID string) error {
	// Delete from waiting_tasks
	_, err := db.Exec(`DELETE FROM waiting_tasks WHERE task_id = $1`, childID)
	if err != nil {
		return fmt.Errorf("failed to delete waiting task: %w", err)
	}

	// Mark all of this child's dependency rows as failed (for tracking)
	_, err = db.Exec(
		`UPDATE task_dependencies SET parent_state = 'cascade_failed' WHERE child_id = $1 AND parent_state = 'pending'`,
		childID,
	)
	if err != nil {
		return fmt.Errorf("failed to update cascade state: %w", err)
	}

	// Recursively resolve this child's own dependents (cascade)
	return ResolveDependencies(db, asynqClient, redisClient, childID, "failed")
}

func enqueueWaitingTask(ctx context.Context, db *sqlx.DB, asynqClient *asynq.Client, redisClient *redis.Client, wt models.WaitingTask) error {
	if asynqClient == nil {
		return fmt.Errorf("asynq client is nil, cannot enqueue task %s", wt.TaskID)
	}
	payload := wt.Payload

	// If inject_parent_results is true, fetch parent results and inject
	if wt.InjectParentResults {
		injected, err := injectParentResults(ctx, db, redisClient, wt.TaskID, payload)
		if err != nil {
			log.Printf("[DEPS] Warning: failed to inject parent results for %s: %v", wt.TaskID, err)
		} else {
			payload = injected
		}
	}

	task, err := NewGenericTask(wt.Queue, payload)
	if err != nil {
		return fmt.Errorf("failed to create task: %w", err)
	}

	opts := []asynq.Option{
		asynq.Timeout(time.Duration(wt.Timeout) * time.Second),
		asynq.Queue(wt.Queue),
		asynq.MaxRetry(3),
		asynq.Retention(24 * time.Hour),
		asynq.TaskID(wt.TaskID),
	}

	_, err = asynqClient.Enqueue(task, opts...)
	if err != nil {
		return fmt.Errorf("failed to enqueue task: %w", err)
	}

	// Store queue metadata for reverse lookup
	redisClient.HSet(ctx, "asynq:t:"+wt.TaskID, "queue", wt.Queue)
	redisClient.SAdd(ctx, "asynq:queues", wt.Queue)

	// Delete from waiting_tasks
	_, err = db.Exec(`DELETE FROM waiting_tasks WHERE task_id = $1`, wt.TaskID)
	if err != nil {
		log.Printf("[DEPS] Warning: failed to delete waiting task %s after enqueue: %v", wt.TaskID, err)
	}

	log.Printf("[DEPS] Enqueued waiting task %s to queue %s", wt.TaskID, wt.Queue)
	return nil
}

func injectParentResults(ctx context.Context, db *sqlx.DB, redisClient *redis.Client, childID string, payload []byte) ([]byte, error) {
	// Get all parent IDs
	var deps []models.TaskDependency
	err := db.Select(&deps, `SELECT * FROM task_dependencies WHERE child_id = $1`, childID)
	if err != nil {
		return nil, err
	}

	parentResults := make(map[string]interface{})
	for _, dep := range deps {
		taskKey := fmt.Sprintf("asynq:t:%s", dep.ParentID)
		result, err := redisClient.HGet(ctx, taskKey, "result").Result()
		if err == redis.Nil {
			parentResults[dep.ParentID] = nil
			continue
		}
		if err != nil {
			parentResults[dep.ParentID] = nil
			continue
		}

		// Try to parse as JSON
		var parsed interface{}
		if json.Unmarshal([]byte(result), &parsed) == nil {
			parentResults[dep.ParentID] = parsed
		} else {
			parentResults[dep.ParentID] = result
		}
	}

	// Inject into payload
	var payloadMap map[string]interface{}
	if err := json.Unmarshal(payload, &payloadMap); err != nil {
		return nil, fmt.Errorf("payload is not a JSON object: %w", err)
	}

	payloadMap["_parent_results"] = parentResults
	return json.Marshal(payloadMap)
}

// StartDependencyResolver runs a periodic background loop that checks for completed
// parent tasks and resolves their dependents.
func StartDependencyResolver(ctx context.Context, db *sqlx.DB, asynqClient *asynq.Client, redisClient *redis.Client, debug bool) {
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()

	if debug {
		log.Println("[DEPS] Background dependency resolver started (2s interval)")
	}

	for {
		select {
		case <-ctx.Done():
			if debug {
				log.Println("[DEPS] Background dependency resolver stopped")
			}
			return
		case <-ticker.C:
			resolveAllPending(db, asynqClient, redisClient)
		}
	}
}

// resolveAllPending checks all pending dependency rows and resolves any whose
// parent tasks have completed in asynq/Redis.
func resolveAllPending(db *sqlx.DB, asynqClient *asynq.Client, redisClient *redis.Client) {
	ctx := context.Background()

	// Find all distinct parent_ids that are still pending
	var parentIDs []string
	err := db.Select(&parentIDs,
		`SELECT DISTINCT parent_id FROM task_dependencies WHERE parent_state = 'pending'`,
	)
	if err != nil || len(parentIDs) == 0 {
		return
	}

	for _, parentID := range parentIDs {
		// Check parent state in Redis
		taskKey := fmt.Sprintf("asynq:t:%s", parentID)
		queue, err := redisClient.HGet(ctx, taskKey, "queue").Result()
		if err != nil {
			continue
		}

		// Use inspector to get task state
		inspector := asynq.NewInspector(asynq.RedisClientOpt{
			Addr:     redisClient.Options().Addr,
			Password: redisClient.Options().Password,
			DB:       redisClient.Options().DB,
		})

		taskInfo, err := inspector.GetTaskInfo(queue, parentID)
		inspector.Close()
		if err != nil {
			continue
		}

		stateStr := taskInfo.State.String()
		// Only resolve for terminal states
		if stateStr == "completed" || stateStr == "archived" || stateStr == "failed" {
			if err := ResolveDependencies(db, asynqClient, redisClient, parentID, stateStr); err != nil {
				log.Printf("[DEPS] Error resolving deps for parent %s: %v", parentID, err)
			}
		}
	}
}
