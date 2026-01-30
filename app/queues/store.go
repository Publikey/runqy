package queueworker

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/hibiken/asynq"
	"github.com/jmoiron/sqlx"
	"github.com/redis/go-redis/v9"
)

// QueueWorkerRow represents a row in the queue_workers_config table
type QueueWorkerRow struct {
	ID         int            `db:"id"`
	Name       sql.NullString `db:"name"`
	Priority   sql.NullInt64  `db:"priority"`
	Deployment sql.NullString `db:"deployment"` // JSON stored as TEXT for SQLite compatibility
}

// Store handles database operations for queue configurations (PostgreSQL or SQLite)
// Redis is still used for asynq-related operations
type Store struct {
	db          *sqlx.DB
	rdb         *redis.Client
	asynqOpt    asynq.RedisClientOpt // For creating asynq inspector
	hasAsynqOpt bool                 // Whether asynqOpt is configured
}

// NewStore creates a new queue worker store
func NewStore(db *sqlx.DB, rdb *redis.Client) *Store {
	return &Store{db: db, rdb: rdb}
}

// NewStoreWithAsynq creates a new queue worker store with asynq inspector support
func NewStoreWithAsynq(db *sqlx.DB, rdb *redis.Client, opt asynq.RedisClientOpt) *Store {
	return &Store{db: db, rdb: rdb, asynqOpt: opt, hasAsynqOpt: true}
}

// SetAsynqOpt sets the asynq Redis client options for inspector operations
func (s *Store) SetAsynqOpt(opt asynq.RedisClientOpt) {
	s.asynqOpt = opt
	s.hasAsynqOpt = true
}

// QueueStats represents task counts for a queue
type QueueStats struct {
	Name      string `json:"name"`
	Pending   int    `json:"pending"`
	Active    int    `json:"active"`
	Scheduled int    `json:"scheduled"`
	Retry     int    `json:"retry"`
	Archived  int    `json:"archived"`
	Completed int    `json:"completed"`
	IsEmpty   bool   `json:"is_empty"`
}

// SyncResult represents the result of a sync operation
type SyncResult struct {
	Orphaned []string `json:"orphaned"` // Queues found only in Redis
	Cleaned  []string `json:"cleaned"`  // Queues that were removed
	Skipped  []string `json:"skipped"`  // Non-empty queues (when force=false)
	Errors   []string `json:"errors"`
}

// Save stores or updates a queue configuration in the database
func (s *Store) Save(ctx context.Context, cfg *QueueConfig) error {
	// Marshal deployment if present
	var deploymentJSON sql.NullString
	var err error
	if cfg.Deployment != nil {
		b, err := json.Marshal(cfg.Deployment)
		if err != nil {
			return fmt.Errorf("failed to marshal deployment: %w", err)
		}
		deploymentJSON = sql.NullString{String: string(b), Valid: true}
	} else {
		// The deployment can be nil if it's only a key API for external services (think Google or OpenAI etc.)
		deploymentJSON = sql.NullString{Valid: false}
	}
	query := s.db.Rebind(`
		INSERT INTO queue_workers_config (name, priority, deployment)
		VALUES (?, ?, ?)
		ON CONFLICT (name) DO UPDATE SET
			priority = excluded.priority,
			deployment = excluded.deployment
	`)

	_, err = s.db.ExecContext(ctx, query,
		cfg.Name,
		cfg.Priority,
		deploymentJSON,
	)
	if err != nil {
		return fmt.Errorf("failed to save config: %w", err)
	}

	return nil
}

// Get retrieves a queue configuration from the database
func (s *Store) Get(ctx context.Context, queueName string) (*QueueConfig, error) {
	query := s.db.Rebind(`
		SELECT id, name, priority, deployment
		FROM queue_workers_config
		WHERE name = ?
	`)

	var row QueueWorkerRow
	err := s.db.GetContext(ctx, &row, query, queueName)
	if err == sql.ErrNoRows {
		return nil, nil // Not found
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get config: %w", err)
	}

	cfg := &QueueConfig{
		Name: row.Name.String,
	}

	if row.Priority.Valid {
		cfg.Priority = int(row.Priority.Int64)
	}

	// Unmarshal deployment if present (stored as TEXT/JSON string)
	if row.Deployment.Valid && row.Deployment.String != "" && row.Deployment.String != "null" {
		var deployment DeploymentConfig
		if err := json.Unmarshal([]byte(row.Deployment.String), &deployment); err == nil {
			cfg.Deployment = &deployment
		}
	}

	return cfg, nil
}

// ListQueues returns all registered queue names
func (s *Store) ListQueues(ctx context.Context) ([]string, error) {
	query := `SELECT name FROM queue_workers_config ORDER BY name`

	// Use a struct wrapper for better SQLite compatibility
	type nameRow struct {
		Name string `db:"name"`
	}
	var rows []nameRow
	err := s.db.SelectContext(ctx, &rows, query)
	if err != nil {
		return nil, fmt.Errorf("failed to list queues: %w", err)
	}

	queues := make([]string, 0, len(rows))
	for _, r := range rows {
		queues = append(queues, r.Name)
	}

	return queues, nil
}

// Delete removes a queue configuration from the database
func (s *Store) Delete(ctx context.Context, queueName string) error {
	query := s.db.Rebind(`DELETE FROM queue_workers_config WHERE name = ?`)
	_, err := s.db.ExecContext(ctx, query, queueName)
	if err != nil {
		return fmt.Errorf("failed to delete config: %w", err)
	}
	return nil
}

// ListByPrefix returns all queue configurations that start with the given prefix
func (s *Store) ListByPrefix(ctx context.Context, prefix string) ([]*QueueConfig, error) {
	query := s.db.Rebind(`
		SELECT id, name, priority, deployment
		FROM queue_workers_config
		WHERE name LIKE ?
		ORDER BY priority DESC
	`)

	var rows []QueueWorkerRow
	err := s.db.SelectContext(ctx, &rows, query, prefix+"%")
	if err != nil {
		return nil, fmt.Errorf("failed to list queues by prefix: %w", err)
	}

	configs := make([]*QueueConfig, 0, len(rows))
	for _, row := range rows {
		cfg := &QueueConfig{
			Name: row.Name.String,
		}

		if row.Priority.Valid {
			cfg.Priority = int(row.Priority.Int64)
		}

		// Unmarshal deployment if present (stored as TEXT/JSON string)
		if row.Deployment.Valid && row.Deployment.String != "" && row.Deployment.String != "null" {
			var deployment DeploymentConfig
			if err := json.Unmarshal([]byte(row.Deployment.String), &deployment); err == nil {
				cfg.Deployment = &deployment
			}
		}

		configs = append(configs, cfg)
	}

	return configs, nil
}

// Exists checks if a queue configuration exists
func (s *Store) Exists(ctx context.Context, queueName string) (bool, error) {
	query := s.db.Rebind(`SELECT EXISTS(SELECT 1 FROM queue_workers_config WHERE name = ?)`)

	var exists bool
	err := s.db.GetContext(ctx, &exists, query, queueName)
	if err != nil {
		return false, fmt.Errorf("failed to check existence: %w", err)
	}
	return exists, nil
}

// --- Redis-only operations (for asynq compatibility) ---

const (
	workerKeyPattern = "asynq:workers:*"
	staleThreshold   = 30 // seconds
	asynqQueuesKey   = "asynq:queues"
)

// RegisterAsynqQueues adds queue names to asynq's queue registry so they appear in asynqmon
func (s *Store) RegisterAsynqQueues(ctx context.Context, queueNames []string) error {
	if len(queueNames) == 0 {
		return nil
	}
	args := make([]interface{}, len(queueNames))
	for i, name := range queueNames {
		args[i] = name
	}
	return s.rdb.SAdd(ctx, asynqQueuesKey, args...).Err()
}

// UnregisterAsynqQueues removes queue names from asynq's queue registry
func (s *Store) UnregisterAsynqQueues(ctx context.Context, queueNames []string) error {
	if len(queueNames) == 0 {
		return nil
	}
	args := make([]interface{}, len(queueNames))
	for i, name := range queueNames {
		args[i] = name
	}
	return s.rdb.SRem(ctx, asynqQueuesKey, args...).Err()
}

// SyncConfigsToAsynq ensures all configs in the database are registered in asynq
func (s *Store) SyncConfigsToAsynq(ctx context.Context) error {
	queues, err := s.ListQueues(ctx)
	if err != nil {
		return err
	}
	if len(queues) > 0 {
		return s.RegisterAsynqQueues(ctx, queues)
	}
	return nil
}

// CleanupStaleWorkers removes worker entries that haven't sent a heartbeat recently
func (s *Store) CleanupStaleWorkers(ctx context.Context) (int, error) {
	// Scan for all worker keys
	var workerKeys []string
	iter := s.rdb.Scan(ctx, 0, workerKeyPattern, 100).Iterator()
	for iter.Next(ctx) {
		workerKeys = append(workerKeys, iter.Val())
	}
	if err := iter.Err(); err != nil {
		return 0, fmt.Errorf("failed to scan workers: %w", err)
	}

	now := time.Now().Unix()
	deleted := 0

	for _, key := range workerKeys {
		// Check if it's a HASH type
		keyType, err := s.rdb.Type(ctx, key).Result()
		if err != nil || keyType != "hash" {
			continue
		}

		// Get last_beat
		lastBeatStr, err := s.rdb.HGet(ctx, key, "last_beat").Result()
		if err != nil {
			// No last_beat field, delete it
			s.rdb.Del(ctx, key)
			deleted++
			continue
		}

		lastBeat, err := strconv.ParseInt(lastBeatStr, 10, 64)
		if err != nil {
			continue
		}

		// If stale, delete it
		if now-lastBeat > staleThreshold {
			s.rdb.Del(ctx, key)
			deleted++
		}
	}

	return deleted, nil
}

// --- Queue sync operations (Redis + DB) ---

// GetQueueStats returns task counts for a queue using asynq inspector
func (s *Store) GetQueueStats(ctx context.Context, queueName string) (*QueueStats, error) {
	if !s.hasAsynqOpt {
		return nil, errors.New("asynq options not configured")
	}

	inspector := asynq.NewInspector(s.asynqOpt)
	defer inspector.Close()

	info, err := inspector.GetQueueInfo(queueName)
	if err != nil {
		if errors.Is(err, asynq.ErrQueueNotFound) {
			// Queue doesn't exist in Redis, return empty stats
			return &QueueStats{
				Name:    queueName,
				IsEmpty: true,
			}, nil
		}
		return nil, fmt.Errorf("failed to get queue info: %w", err)
	}

	stats := &QueueStats{
		Name:      queueName,
		Pending:   info.Pending,
		Active:    info.Active,
		Scheduled: info.Scheduled,
		Retry:     info.Retry,
		Archived:  info.Archived,
		Completed: info.Completed,
	}
	stats.IsEmpty = stats.Pending == 0 && stats.Active == 0 && stats.Scheduled == 0 && stats.Retry == 0

	return stats, nil
}

// ListOrphanedQueues returns queues that exist in Redis but not in the database
func (s *Store) ListOrphanedQueues(ctx context.Context) ([]string, error) {
	// Get queues from Redis (asynq:queues set)
	redisQueues, err := s.rdb.SMembers(ctx, asynqQueuesKey).Result()
	if err != nil {
		return nil, fmt.Errorf("failed to get queues from Redis: %w", err)
	}

	// Get queues from database
	dbQueues, err := s.ListQueues(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get queues from database: %w", err)
	}

	// Create a set of DB queues for fast lookup
	dbQueueSet := make(map[string]bool)
	for _, q := range dbQueues {
		dbQueueSet[q] = true
	}

	// Find orphaned queues (in Redis but not in DB)
	var orphaned []string
	for _, q := range redisQueues {
		if !dbQueueSet[q] {
			orphaned = append(orphaned, q)
		}
	}

	return orphaned, nil
}

// CleanupOrphanedQueues removes orphaned queues from Redis
// If force is false, only empty queues are removed
func (s *Store) CleanupOrphanedQueues(ctx context.Context, force bool) (*SyncResult, error) {
	if !s.hasAsynqOpt {
		return nil, errors.New("asynq options not configured")
	}

	orphaned, err := s.ListOrphanedQueues(ctx)
	if err != nil {
		return nil, err
	}

	result := &SyncResult{
		Orphaned: orphaned,
	}

	if len(orphaned) == 0 {
		return result, nil
	}

	inspector := asynq.NewInspector(s.asynqOpt)
	defer inspector.Close()

	for _, queueName := range orphaned {
		// Get queue stats to check if empty
		stats, err := s.GetQueueStats(ctx, queueName)
		if err != nil {
			result.Errors = append(result.Errors, fmt.Sprintf("%s: failed to get stats: %v", queueName, err))
			continue
		}

		// Skip non-empty queues unless force is true
		if !stats.IsEmpty && !force {
			result.Skipped = append(result.Skipped, queueName)
			continue
		}

		// Delete the queue from Redis
		if err := inspector.DeleteQueue(queueName, force); err != nil {
			if errors.Is(err, asynq.ErrQueueNotEmpty) {
				result.Skipped = append(result.Skipped, queueName)
			} else {
				result.Errors = append(result.Errors, fmt.Sprintf("%s: %v", queueName, err))
			}
			continue
		}

		result.Cleaned = append(result.Cleaned, queueName)
	}

	return result, nil
}

// DeleteQueueFull removes a queue from both the database and Redis
// If force is false, deletion fails if the queue has pending tasks
func (s *Store) DeleteQueueFull(ctx context.Context, queueName string, force bool) error {
	if !s.hasAsynqOpt {
		return errors.New("asynq options not configured")
	}

	// Delete from database first
	if err := s.Delete(ctx, queueName); err != nil {
		return fmt.Errorf("failed to delete from database: %w", err)
	}

	// Delete from Redis using asynq inspector (handles all task data cleanup)
	inspector := asynq.NewInspector(s.asynqOpt)
	defer inspector.Close()

	if err := inspector.DeleteQueue(queueName, force); err != nil {
		// If queue doesn't exist in Redis, that's fine
		if errors.Is(err, asynq.ErrQueueNotFound) {
			return nil
		}
		// If queue is not empty and force is false, restore the DB entry would be complex
		// For now, just return the error - the DB entry is already deleted
		if errors.Is(err, asynq.ErrQueueNotEmpty) {
			return fmt.Errorf("queue has pending tasks, use --force to delete anyway")
		}
		return fmt.Errorf("failed to delete from Redis: %w", err)
	}

	return nil
}
