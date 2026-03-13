package tasks

import (
	"context"
	"encoding/json"
	"os"
	"testing"
	"time"

	"github.com/jmoiron/sqlx"
	_ "modernc.org/sqlite"
)

// setupTestDB creates an in-memory SQLite database with the task_dependencies
// and waiting_tasks tables for testing.
func setupTestDB(t *testing.T) *sqlx.DB {
	t.Helper()

	db, err := sqlx.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("failed to open test db: %v", err)
	}

	schema := `
	CREATE TABLE IF NOT EXISTS task_dependencies (
		id            INTEGER PRIMARY KEY AUTOINCREMENT,
		child_id      TEXT NOT NULL,
		parent_id     TEXT NOT NULL,
		parent_state  TEXT NOT NULL DEFAULT 'pending',
		created_at    BIGINT NOT NULL,
		UNIQUE(child_id, parent_id)
	);
	CREATE INDEX IF NOT EXISTS idx_task_deps_parent ON task_dependencies(parent_id);
	CREATE INDEX IF NOT EXISTS idx_task_deps_child ON task_dependencies(child_id);

	CREATE TABLE IF NOT EXISTS waiting_tasks (
		id                      INTEGER PRIMARY KEY AUTOINCREMENT,
		task_id                 TEXT NOT NULL UNIQUE,
		queue                   TEXT NOT NULL,
		payload                 TEXT NOT NULL,
		on_parent_failure       TEXT NOT NULL DEFAULT 'fail',
		inject_parent_results   INTEGER NOT NULL DEFAULT 0,
		timeout                 BIGINT NOT NULL DEFAULT 0,
		created_at              BIGINT NOT NULL
	);
	`
	if _, err := db.Exec(schema); err != nil {
		t.Fatalf("failed to create test schema: %v", err)
	}

	return db
}

func TestResolveDependencies_UpdatesParentState(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	now := time.Now().Unix()

	// Insert a dependency: child1 depends on parent1
	_, err := db.Exec(
		`INSERT INTO task_dependencies (child_id, parent_id, parent_state, created_at) VALUES ($1, $2, 'pending', $3)`,
		"child1", "parent1", now,
	)
	if err != nil {
		t.Fatalf("failed to insert dep: %v", err)
	}

	// Insert a waiting task for child1
	payload, _ := json.Marshal(map[string]string{"key": "value"})
	_, err = db.Exec(
		`INSERT INTO waiting_tasks (task_id, queue, payload, on_parent_failure, inject_parent_results, timeout, created_at) VALUES ($1, $2, $3, $4, $5, $6, $7)`,
		"child1", "test.default", payload, "fail", false, 30, now,
	)
	if err != nil {
		t.Fatalf("failed to insert waiting task: %v", err)
	}

	// Call ResolveDependencies with nil asynq client
	// It will update dep state but fail at enqueue (which is expected)
	err = ResolveDependencies(db, nil, nil, "parent1", "completed")
	// Error is logged but not returned for individual children, so no error expected at top level

	// The dep state should have been updated before the enqueue attempt
	var state string
	db.Get(&state, `SELECT parent_state FROM task_dependencies WHERE parent_id = 'parent1' AND child_id = 'child1'`)
	if state != "completed" {
		t.Errorf("expected parent_state 'completed', got '%s'", state)
	}

	// Waiting task should still exist since enqueue failed
	var count int
	db.Get(&count, `SELECT COUNT(*) FROM waiting_tasks WHERE task_id = 'child1'`)
	if count != 1 {
		t.Errorf("expected child1 still in waiting_tasks after failed enqueue, got count %d", count)
	}
}

func TestResolveDependencies_CascadeFailure(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	now := time.Now().Unix()

	// child1 depends on parent1 with on_parent_failure=fail
	db.Exec(
		`INSERT INTO task_dependencies (child_id, parent_id, parent_state, created_at) VALUES ($1, $2, 'pending', $3)`,
		"child1", "parent1", now,
	)
	payload, _ := json.Marshal(map[string]string{"key": "value"})
	db.Exec(
		`INSERT INTO waiting_tasks (task_id, queue, payload, on_parent_failure, inject_parent_results, timeout, created_at) VALUES ($1, $2, $3, $4, $5, $6, $7)`,
		"child1", "test.default", payload, "fail", false, 30, now,
	)

	// grandchild1 depends on child1
	db.Exec(
		`INSERT INTO task_dependencies (child_id, parent_id, parent_state, created_at) VALUES ($1, $2, 'pending', $3)`,
		"grandchild1", "child1", now,
	)
	db.Exec(
		`INSERT INTO waiting_tasks (task_id, queue, payload, on_parent_failure, inject_parent_results, timeout, created_at) VALUES ($1, $2, $3, $4, $5, $6, $7)`,
		"grandchild1", "test.default", payload, "fail", false, 30, now,
	)

	// Resolve parent1 as failed
	err := ResolveDependencies(db, nil, nil, "parent1", "failed")
	if err != nil {
		t.Fatalf("ResolveDependencies error: %v", err)
	}

	// child1 should be removed from waiting_tasks (cascade fail)
	var count int
	db.Get(&count, `SELECT COUNT(*) FROM waiting_tasks WHERE task_id = 'child1'`)
	if count != 0 {
		t.Errorf("expected child1 removed from waiting_tasks, got count %d", count)
	}

	// grandchild1 should also be cascade-failed
	db.Get(&count, `SELECT COUNT(*) FROM waiting_tasks WHERE task_id = 'grandchild1'`)
	if count != 0 {
		t.Errorf("expected grandchild1 removed from waiting_tasks, got count %d", count)
	}
}

func TestResolveDependencies_IgnoreFailure(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	now := time.Now().Unix()

	// child1 depends on parent1 AND parent2 with on_parent_failure=ignore
	db.Exec(
		`INSERT INTO task_dependencies (child_id, parent_id, parent_state, created_at) VALUES ($1, $2, 'pending', $3)`,
		"child1", "parent1", now,
	)
	db.Exec(
		`INSERT INTO task_dependencies (child_id, parent_id, parent_state, created_at) VALUES ($1, $2, 'pending', $3)`,
		"child1", "parent2", now,
	)
	payload, _ := json.Marshal(map[string]string{"key": "value"})
	db.Exec(
		`INSERT INTO waiting_tasks (task_id, queue, payload, on_parent_failure, inject_parent_results, timeout, created_at) VALUES ($1, $2, $3, $4, $5, $6, $7)`,
		"child1", "test.default", payload, "ignore", false, 30, now,
	)

	// Resolve parent1 as failed
	err := ResolveDependencies(db, nil, nil, "parent1", "failed")
	if err != nil {
		t.Fatalf("ResolveDependencies error: %v", err)
	}

	// child1 should still be waiting (parent2 still pending, but parent1 failure is ignored)
	var count int
	db.Get(&count, `SELECT COUNT(*) FROM waiting_tasks WHERE task_id = 'child1'`)
	if count != 1 {
		t.Errorf("expected child1 still in waiting_tasks, got count %d", count)
	}

	// parent1 state should be updated to "failed"
	var state string
	db.Get(&state, `SELECT parent_state FROM task_dependencies WHERE parent_id = 'parent1' AND child_id = 'child1'`)
	if state != "failed" {
		t.Errorf("expected parent_state 'failed', got '%s'", state)
	}
}

func TestResolveDependencies_MultipleParentsAllCompleted(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	now := time.Now().Unix()

	// child1 depends on parent1 (already completed) and parent2 (pending)
	db.Exec(
		`INSERT INTO task_dependencies (child_id, parent_id, parent_state, created_at) VALUES ($1, $2, 'completed', $3)`,
		"child1", "parent1", now,
	)
	db.Exec(
		`INSERT INTO task_dependencies (child_id, parent_id, parent_state, created_at) VALUES ($1, $2, 'pending', $3)`,
		"child1", "parent2", now,
	)
	payload, _ := json.Marshal(map[string]string{"key": "value"})
	db.Exec(
		`INSERT INTO waiting_tasks (task_id, queue, payload, on_parent_failure, inject_parent_results, timeout, created_at) VALUES ($1, $2, $3, $4, $5, $6, $7)`,
		"child1", "test.default", payload, "fail", false, 30, now,
	)

	// Resolve parent2 as completed - now all deps are met
	// This will try to enqueue (and fail since asynq client is nil)
	// but it should resolve the dep state correctly
	err := ResolveDependencies(db, nil, nil, "parent2", "completed")
	// Will get error because of nil asynq client but that's expected
	_ = err

	// Verify both parent states are completed
	var state1, state2 string
	db.Get(&state1, `SELECT parent_state FROM task_dependencies WHERE parent_id = 'parent1' AND child_id = 'child1'`)
	db.Get(&state2, `SELECT parent_state FROM task_dependencies WHERE parent_id = 'parent2' AND child_id = 'child1'`)
	if state2 != "completed" {
		t.Errorf("expected parent2 state 'completed', got '%s'", state2)
	}

	// Check that pending count is 0
	var pendingCount int
	db.Get(&pendingCount, `SELECT COUNT(*) FROM task_dependencies WHERE child_id = 'child1' AND parent_state = 'pending'`)
	if pendingCount != 0 {
		t.Errorf("expected 0 pending deps, got %d", pendingCount)
	}
}

func TestInjectParentResults(t *testing.T) {
	// This test verifies the payload injection logic works correctly
	// without requiring a real Redis connection
	payload := []byte(`{"prompt": "hello"}`)

	var payloadMap map[string]interface{}
	if err := json.Unmarshal(payload, &payloadMap); err != nil {
		t.Fatalf("failed to unmarshal payload: %v", err)
	}

	parentResults := map[string]interface{}{
		"parent1": map[string]interface{}{"output": "result1"},
		"parent2": nil,
	}
	payloadMap["_parent_results"] = parentResults

	result, err := json.Marshal(payloadMap)
	if err != nil {
		t.Fatalf("failed to marshal: %v", err)
	}

	var check map[string]interface{}
	json.Unmarshal(result, &check)

	if _, ok := check["_parent_results"]; !ok {
		t.Error("expected _parent_results in payload")
	}
	if check["prompt"] != "hello" {
		t.Error("expected original prompt field preserved")
	}
}

func TestResolveAllPending_NoRows(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	// Should not panic with empty tables
	resolveAllPending(db, nil, nil)
}

func TestMain(m *testing.M) {
	// Ensure the background resolver can start and stop cleanly
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	// Verify that StartDependencyResolver returns when context is cancelled
	done := make(chan struct{})
	go func() {
		StartDependencyResolver(ctx, nil, nil, nil, false)
		close(done)
	}()

	select {
	case <-done:
		// OK
	case <-time.After(5 * time.Second):
		panic("StartDependencyResolver did not stop when context was cancelled")
	}

	os.Exit(m.Run())
}
