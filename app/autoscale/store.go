// Package autoscale implements GPU worker auto-scaling: a background control loop that
// provisions and destroys cloud GPU workers per queue based on demand, a store for tracked
// instances, and coordination with the worker heartbeat for instance correlation and
// protection.
package autoscale

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jmoiron/sqlx"
)

// Instance lifecycle states.
const (
	StatusProvisioning = "provisioning"
	StatusRunning      = "running"
	StatusDraining     = "draining"
	StatusDestroying   = "destroying"
	StatusTerminated   = "terminated"
	StatusFailed       = "failed"
)

// ErrInstanceNotFound is returned when an instance row does not exist.
var ErrInstanceNotFound = errors.New("autoscale instance not found")

// Instance is a tracked, autoscaler-managed cloud GPU worker instance.
type Instance struct {
	ID              int64      `json:"id" db:"id"`
	InstanceID      string     `json:"instance_id" db:"instance_id"`
	Provider        string     `json:"provider" db:"provider"`
	Queue           string     `json:"queue" db:"queue"`
	Status          string     `json:"status" db:"status"`
	WorkerID        string     `json:"worker_id" db:"worker_id"`
	Protected       bool       `json:"protected" db:"protected"`
	PricePerHour    float64    `json:"price_per_hour" db:"price_per_hour"`
	CostAccumulated float64    `json:"cost_accumulated" db:"cost_accumulated"`
	Metadata        []byte     `json:"-" db:"metadata"`
	CreatedAt       time.Time  `json:"created_at" db:"created_at"`
	LastJobAt       *time.Time `json:"last_job_at,omitempty" db:"last_job_at"`
	UpdatedAt       time.Time  `json:"updated_at" db:"updated_at"`
}

// IsLive reports whether the instance is providing (or about to provide) capacity, i.e.
// provisioning or running. Draining/destroying/terminated/failed are not counted.
func (i *Instance) IsLive() bool {
	return i.Status == StatusProvisioning || i.Status == StatusRunning
}

// Store persists autoscaler-managed instances in the autoscale_instances table.
type Store struct {
	db *sqlx.DB
}

// NewStore creates an instance store.
func NewStore(db *sqlx.DB) *Store { return &Store{db: db} }

// Create inserts a new instance row.
func (s *Store) Create(ctx context.Context, inst *Instance) error {
	now := time.Now()
	if inst.CreatedAt.IsZero() {
		inst.CreatedAt = now
	}
	inst.UpdatedAt = now
	if inst.Status == "" {
		inst.Status = StatusProvisioning
	}
	if s.db.DriverName() == "sqlite" {
		res, err := s.db.ExecContext(ctx,
			`INSERT INTO autoscale_instances (instance_id, provider, queue, status, worker_id, protected, price_per_hour, cost_accumulated, metadata, created_at, updated_at)
			 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			inst.InstanceID, inst.Provider, inst.Queue, inst.Status, inst.WorkerID, inst.Protected,
			inst.PricePerHour, inst.CostAccumulated, nullBytes(inst.Metadata), inst.CreatedAt, inst.UpdatedAt)
		if err != nil {
			return fmt.Errorf("failed to create instance: %w", err)
		}
		inst.ID, _ = res.LastInsertId()
		return nil
	}
	err := s.db.QueryRowContext(ctx,
		`INSERT INTO autoscale_instances (instance_id, provider, queue, status, worker_id, protected, price_per_hour, cost_accumulated, metadata, created_at, updated_at)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11) RETURNING id`,
		inst.InstanceID, inst.Provider, inst.Queue, inst.Status, inst.WorkerID, inst.Protected,
		inst.PricePerHour, inst.CostAccumulated, nullBytes(inst.Metadata), inst.CreatedAt, inst.UpdatedAt).Scan(&inst.ID)
	if err != nil {
		return fmt.Errorf("failed to create instance: %w", err)
	}
	return nil
}

func nullBytes(b []byte) interface{} {
	if len(b) == 0 {
		return nil
	}
	return b
}

const instanceCols = `id, instance_id, provider, queue, status, worker_id, protected, price_per_hour, cost_accumulated, metadata, created_at, last_job_at, updated_at`

// GetByInstanceID returns one instance by provider instance id, or nil if not found.
func (s *Store) GetByInstanceID(ctx context.Context, instanceID string) (*Instance, error) {
	query := s.db.Rebind(`SELECT ` + instanceCols + ` FROM autoscale_instances WHERE instance_id = ?`)
	var inst Instance
	err := s.db.GetContext(ctx, &inst, query, instanceID)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get instance: %w", err)
	}
	return &inst, nil
}

// ListByQueue returns all non-terminal instances for a queue (provisioning..draining..destroying).
func (s *Store) ListByQueue(ctx context.Context, queue string) ([]*Instance, error) {
	query := s.db.Rebind(`SELECT ` + instanceCols + ` FROM autoscale_instances WHERE queue = ? AND status NOT IN ('terminated','failed') ORDER BY created_at`)
	return s.selectInstances(ctx, query, queue)
}

// ListActive returns all non-terminal instances across all queues.
func (s *Store) ListActive(ctx context.Context) ([]*Instance, error) {
	query := `SELECT ` + instanceCols + ` FROM autoscale_instances WHERE status NOT IN ('terminated','failed') ORDER BY queue, created_at`
	return s.selectInstances(ctx, query)
}

// ListAll returns every tracked instance (including terminated), most recent first.
func (s *Store) ListAll(ctx context.Context) ([]*Instance, error) {
	query := `SELECT ` + instanceCols + ` FROM autoscale_instances ORDER BY created_at DESC`
	return s.selectInstances(ctx, query)
}

func (s *Store) selectInstances(ctx context.Context, query string, args ...interface{}) ([]*Instance, error) {
	var rows []Instance
	if err := s.db.SelectContext(ctx, &rows, query, args...); err != nil {
		return nil, fmt.Errorf("failed to list instances: %w", err)
	}
	out := make([]*Instance, len(rows))
	for i := range rows {
		inst := rows[i]
		out[i] = &inst
	}
	return out, nil
}

// UpdateStatus sets the status of an instance.
func (s *Store) UpdateStatus(ctx context.Context, instanceID, status string) error {
	query := s.db.Rebind(`UPDATE autoscale_instances SET status = ?, updated_at = ? WHERE instance_id = ?`)
	return s.exec(ctx, query, status, time.Now(), instanceID)
}

// SetWorkerID associates a registered worker with an instance (correlation).
func (s *Store) SetWorkerID(ctx context.Context, instanceID, workerID string) error {
	query := s.db.Rebind(`UPDATE autoscale_instances SET worker_id = ?, updated_at = ? WHERE instance_id = ?`)
	return s.exec(ctx, query, workerID, time.Now(), instanceID)
}

// SetProtected toggles the protection flag of an instance.
func (s *Store) SetProtected(ctx context.Context, instanceID string, protected bool) error {
	query := s.db.Rebind(`UPDATE autoscale_instances SET protected = ?, updated_at = ? WHERE instance_id = ?`)
	res, err := s.db.ExecContext(ctx, query, protected, time.Now(), instanceID)
	if err != nil {
		return fmt.Errorf("failed to set protected: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrInstanceNotFound
	}
	return nil
}

// MarkJobActivity records that the instance's worker has activity now (resets idle timer).
func (s *Store) MarkJobActivity(ctx context.Context, instanceID string, ts time.Time) error {
	query := s.db.Rebind(`UPDATE autoscale_instances SET last_job_at = ?, updated_at = ? WHERE instance_id = ?`)
	return s.exec(ctx, query, ts, time.Now(), instanceID)
}

// AddCost accumulates cost onto an instance.
func (s *Store) AddCost(ctx context.Context, instanceID string, delta float64) error {
	query := s.db.Rebind(`UPDATE autoscale_instances SET cost_accumulated = cost_accumulated + ?, updated_at = ? WHERE instance_id = ?`)
	return s.exec(ctx, query, delta, time.Now(), instanceID)
}

func (s *Store) exec(ctx context.Context, query string, args ...interface{}) error {
	if _, err := s.db.ExecContext(ctx, query, args...); err != nil {
		return fmt.Errorf("instance update failed: %w", err)
	}
	return nil
}

// MetadataMap unmarshals an instance's metadata JSON into a map (nil if empty/invalid).
func (i *Instance) MetadataMap() map[string]interface{} {
	if len(i.Metadata) == 0 {
		return nil
	}
	var m map[string]interface{}
	if err := json.Unmarshal(i.Metadata, &m); err != nil {
		return nil
	}
	return m
}
