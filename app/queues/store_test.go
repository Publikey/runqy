package queueworker

import (
	"context"
	"os"
	"testing"

	"github.com/jmoiron/sqlx"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	_ "modernc.org/sqlite"
)

// setupTestDB creates an in-memory SQLite database with the queue schema
func setupTestDB(t *testing.T) *sqlx.DB {
	t.Helper()

	// Use a temp file so multiple connections see the same data
	tmpFile, err := os.CreateTemp("", "store-test-*.db")
	require.NoError(t, err)
	tmpFile.Close()
	t.Cleanup(func() { os.Remove(tmpFile.Name()) })

	db, err := sqlx.Connect("sqlite", tmpFile.Name())
	require.NoError(t, err)
	t.Cleanup(func() { db.Close() })

	_, err = db.Exec("PRAGMA foreign_keys = ON")
	require.NoError(t, err)

	// Create schema (SQLite version from models/schema.go)
	_, err = db.Exec(`
		CREATE TABLE IF NOT EXISTS queues (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			name TEXT UNIQUE NOT NULL,
			provider TEXT DEFAULT '',
			deployment TEXT,
			input_schema TEXT,
			output_schema TEXT,
			description TEXT DEFAULT '',
			enabled INTEGER DEFAULT 1,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
		);
		CREATE TABLE IF NOT EXISTS sub_queues (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			queue_id INTEGER NOT NULL REFERENCES queues(id) ON DELETE CASCADE,
			name TEXT NOT NULL,
			priority INTEGER NOT NULL DEFAULT 1,
			enabled INTEGER DEFAULT 1,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			updated_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			UNIQUE(queue_id, name)
		);
	`)
	require.NoError(t, err)

	return db
}

func newTestStore(t *testing.T) *Store {
	t.Helper()
	db := setupTestDB(t)
	return NewStore(db, nil) // nil redis — not needed for DB-only tests
}

// TestSaveSubQueue_ReEnablesOnReCreation verifies that SaveSubQueue sets
// enabled=true when re-inserting a sub-queue that was previously soft-deleted.
func TestSaveSubQueue_ReEnablesOnReCreation(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	// 1. Create a parent queue
	queue := &Queue{Name: "test-queue", Enabled: true}
	queueID, err := store.SaveQueue(ctx, queue)
	require.NoError(t, err)

	// 2. Create a sub-queue
	sq := &SubQueue{Name: "high", Priority: 10, Enabled: true}
	err = store.SaveSubQueue(ctx, queueID, sq)
	require.NoError(t, err)

	// Verify it exists and is enabled
	subQueues, err := store.ListSubQueues(ctx, queueID)
	require.NoError(t, err)
	require.Len(t, subQueues, 1)
	assert.True(t, subQueues[0].Enabled)
	assert.Equal(t, "high", subQueues[0].Name)

	// 3. Soft-delete the sub-queue (simulating config remove)
	err = store.DisableSubQueue(ctx, queueID, "high")
	require.NoError(t, err)

	// Verify it's no longer in the enabled list
	subQueues, err = store.ListSubQueues(ctx, queueID)
	require.NoError(t, err)
	assert.Len(t, subQueues, 0, "disabled sub-queue should not appear in ListSubQueues")

	// Verify it IS in the disabled list
	disabled, err := store.ListDisabledSubQueues(ctx, queueID)
	require.NoError(t, err)
	require.Len(t, disabled, 1)
	assert.False(t, disabled[0].Enabled)

	// 4. Re-create the sub-queue via SaveSubQueue (simulating config create)
	sq2 := &SubQueue{Name: "high", Priority: 5, Enabled: true}
	err = store.SaveSubQueue(ctx, queueID, sq2)
	require.NoError(t, err)

	// 5. Verify it's enabled again with updated priority
	subQueues, err = store.ListSubQueues(ctx, queueID)
	require.NoError(t, err)
	require.Len(t, subQueues, 1, "sub-queue should be re-enabled after SaveSubQueue")
	assert.True(t, subQueues[0].Enabled)
	assert.Equal(t, 5, subQueues[0].Priority, "priority should be updated")
}

// TestSave_ReEnablesSubQueueOnReCreation tests the backward-compatible Save()
// method also re-enables sub-queues after soft-delete.
func TestSave_ReEnablesSubQueueOnReCreation(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	// 1. Create via Save (backward compat)
	cfg := &QueueConfig{
		Name:     "inference.high",
		Priority: 10,
		Deployment: &DeploymentConfig{
			GitURL:     "https://github.com/test/repo",
			StartupCmd: "python main.py",
		},
	}
	err := store.Save(ctx, cfg)
	require.NoError(t, err)

	// Verify it exists
	got, err := store.Get(ctx, "inference.high")
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Equal(t, 10, got.Priority)

	// 2. Soft-delete the entire parent queue (simulating config remove)
	err = store.DeleteQueue(ctx, "inference")
	require.NoError(t, err)

	// Verify it's gone from enabled queries
	got, err = store.Get(ctx, "inference.high")
	require.NoError(t, err)
	assert.Nil(t, got, "should not find disabled queue via Get")

	// 3. Re-create via Save
	cfg2 := &QueueConfig{
		Name:     "inference.high",
		Priority: 20,
		Deployment: &DeploymentConfig{
			GitURL:     "https://github.com/test/repo",
			StartupCmd: "python main.py",
		},
	}
	err = store.Save(ctx, cfg2)
	require.NoError(t, err)

	// 4. Verify it's back and enabled
	got, err = store.Get(ctx, "inference.high")
	require.NoError(t, err)
	require.NotNil(t, got, "queue should be re-enabled after Save")
	assert.Equal(t, 20, got.Priority)
}

// TestListAllSubQueuesForQueue_ReturnsDisabled verifies that
// ListAllSubQueuesForQueue returns sub-queues regardless of enabled state.
func TestListAllSubQueuesForQueue_ReturnsDisabled(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	// Create parent queue with two sub-queues
	queue := &Queue{Name: "multi", Enabled: true}
	queueID, err := store.SaveQueue(ctx, queue)
	require.NoError(t, err)

	err = store.SaveSubQueue(ctx, queueID, &SubQueue{Name: "high", Priority: 10, Enabled: true})
	require.NoError(t, err)
	err = store.SaveSubQueue(ctx, queueID, &SubQueue{Name: "low", Priority: 1, Enabled: true})
	require.NoError(t, err)

	// Disable one sub-queue
	err = store.DisableSubQueue(ctx, queueID, "low")
	require.NoError(t, err)

	// ListSubQueues should only return enabled ones
	enabled, err := store.ListSubQueues(ctx, queueID)
	require.NoError(t, err)
	assert.Len(t, enabled, 1)
	assert.Equal(t, "high", enabled[0].Name)

	// ListAllSubQueuesForQueue should return ALL (both enabled and disabled)
	all, err := store.ListAllSubQueuesForQueue(ctx, queueID)
	require.NoError(t, err)
	assert.Len(t, all, 2, "ListAllSubQueuesForQueue should return both enabled and disabled sub-queues")

	// Verify we got both and their states are correct
	nameMap := map[string]bool{}
	for _, sq := range all {
		nameMap[sq.Name] = sq.Enabled
	}
	assert.True(t, nameMap["high"], "high should be enabled")
	assert.False(t, nameMap["low"], "low should be disabled")
}

// TestListAllSubQueuesForQueue_DeploymentVisibleWithDisabledSubQueues verifies
// that deployment config can be retrieved via the parent queue even when all
// sub-queues are disabled (the core Part B fix).
func TestListAllSubQueuesForQueue_DeploymentVisibleWithDisabledSubQueues(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	// Create queue with deployment and redis_storage
	redisStorage := true
	deployment := &DeploymentConfig{
		GitURL:       "https://github.com/test/repo",
		StartupCmd:   "python main.py",
		RedisStorage: &redisStorage,
	}
	queue := &Queue{Name: "storage-test", Deployment: deployment, Enabled: true}
	queueID, err := store.SaveQueue(ctx, queue)
	require.NoError(t, err)

	err = store.SaveSubQueue(ctx, queueID, &SubQueue{Name: "default", Priority: 1, Enabled: true})
	require.NoError(t, err)

	// Disable ALL sub-queues
	err = store.DisableSubQueue(ctx, queueID, "default")
	require.NoError(t, err)

	// ListQueues (used by old handler) should return nothing
	queues, err := store.ListQueues(ctx)
	require.NoError(t, err)
	assert.Len(t, queues, 0, "ListQueues should not return queues with all sub-queues disabled")

	// But ListParentQueues + GetQueue should still find the parent
	parents, err := store.ListParentQueues(ctx)
	require.NoError(t, err)
	require.Len(t, parents, 1)

	parentQueue, err := store.GetQueue(ctx, parents[0])
	require.NoError(t, err)
	require.NotNil(t, parentQueue)
	require.NotNil(t, parentQueue.Deployment)
	require.NotNil(t, parentQueue.Deployment.RedisStorage, "redis_storage should be accessible from parent queue")
	assert.True(t, *parentQueue.Deployment.RedisStorage, "redis_storage should be true")

	// ListAllSubQueuesForQueue should return the disabled sub-queue
	allSubs, err := store.ListAllSubQueuesForQueue(ctx, parentQueue.ID)
	require.NoError(t, err)
	assert.Len(t, allSubs, 1, "should find disabled sub-queue via ListAllSubQueuesForQueue")
	assert.Equal(t, "default", allSubs[0].Name)
	assert.False(t, allSubs[0].Enabled)
}

// TestToQueueAndSubQueues_SetsEnabledTrue verifies that the loader sets
// Enabled=true on all generated SubQueues.
func TestToQueueAndSubQueues_SetsEnabledTrue(t *testing.T) {
	// Test with explicit sub-queues
	yamlQ := &QueueYAML{
		Priority: 1,
		Deployment: &DeploymentYAML{
			GitURL:     "https://github.com/test/repo",
			StartupCmd: "python main.py",
		},
		SubQueues: []SubQueueYAML{
			{Name: "high", Priority: 10},
			{Name: "low", Priority: 1},
		},
	}

	queue, subQueues := yamlQ.ToQueueAndSubQueues("test")
	assert.True(t, queue.Enabled)
	require.Len(t, subQueues, 2)
	for _, sq := range subQueues {
		assert.True(t, sq.Enabled, "sub-queue %s should have Enabled=true", sq.Name)
	}

	// Test with default sub-queue (no explicit sub-queues)
	yamlQ2 := &QueueYAML{
		Priority: 5,
		Deployment: &DeploymentYAML{
			GitURL:     "https://github.com/test/repo",
			StartupCmd: "python main.py",
		},
	}

	queue2, subQueues2 := yamlQ2.ToQueueAndSubQueues("simple")
	assert.True(t, queue2.Enabled)
	require.Len(t, subQueues2, 1)
	assert.Equal(t, DefaultSubQueueName, subQueues2[0].Name)
	assert.True(t, subQueues2[0].Enabled, "default sub-queue should have Enabled=true")
}

// TestSave_PreservesSchemasAndDescription verifies that Save (used by the
// dashboard, public API and CLI create/update paths, whose requests carry no
// input/output schemas) does not wipe input_schema/output_schema/description
// previously loaded from YAML. Regression test: every dashboard config edit
// used to null these columns.
func TestSave_PreservesSchemasAndDescription(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	required := true
	queue := &Queue{
		Name:         "sdxl",
		InputSchema:  []FieldSchema{{Name: "prompt", Type: []string{"string"}, Required: &required}},
		OutputSchema: []FieldSchema{{Name: "data", Type: []string{"array"}}},
		Description:  "image generation",
		Enabled:      true,
	}
	_, err := store.SaveQueue(ctx, queue)
	require.NoError(t, err)

	// Update via Save, as the dashboard "Edit Config" does (priority + deployment only).
	err = store.Save(ctx, &QueueConfig{
		Name:     "sdxl.priority",
		Priority: 6,
		Deployment: &DeploymentConfig{
			GitURL:     "https://github.com/test/repo",
			StartupCmd: "python main.py",
		},
	})
	require.NoError(t, err)

	got, err := store.GetQueue(ctx, "sdxl")
	require.NoError(t, err)
	require.NotNil(t, got)
	require.Len(t, got.InputSchema, 1, "input_schema must survive a dashboard config edit")
	assert.Equal(t, "prompt", got.InputSchema[0].Name)
	require.Len(t, got.OutputSchema, 1, "output_schema must survive a dashboard config edit")
	assert.Equal(t, "image generation", got.Description)
	require.NotNil(t, got.Deployment, "deployment must be updated by Save")
	assert.Equal(t, "https://github.com/test/repo", got.Deployment.GitURL)
}
