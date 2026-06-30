package provider

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/Publikey/runqy/vaults"
	"github.com/jmoiron/sqlx"
)

var (
	// ErrProviderNotFound is returned when a named provider does not exist.
	ErrProviderNotFound = errors.New("autoscale provider not found")
	// ErrProvidersDisabled is returned when encryption (the vault master key) is not configured.
	ErrProvidersDisabled = errors.New("autoscale providers require encryption: set RUNQY_VAULT_MASTER_KEY")

	validProviderName = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_-]{0,63}$`)

	// secretKeyHint matches config keys whose values should be masked in API responses.
	secretKeyHint = regexp.MustCompile(`(?i)(key|token|secret|password|pass|credential)`)
)

// Record is a provider configuration row. Config holds the DECRYPTED JSON blob; callers
// that expose it over the API must mask it first (see MaskConfig).
type Record struct {
	ID           int64           `json:"-"`
	Name         string          `json:"name"`
	ProviderType string          `json:"provider_type"`
	Config       json.RawMessage `json:"config"`
	Enabled      bool            `json:"enabled"`
	CreatedAt    time.Time       `json:"created_at"`
	UpdatedAt    time.Time       `json:"updated_at"`
}

// Store persists provider configs in the autoscale_providers table. Credentials are
// encrypted at rest with the shared encryption primitive (vaults.GetEncryptor) WITHOUT
// using the vault store or vault:// namespace.
type Store struct {
	db        *sqlx.DB
	encryptor *vaults.Encryptor
}

// NewStore creates a provider-config store sharing the global encryptor.
func NewStore(db *sqlx.DB) *Store {
	return &Store{db: db, encryptor: vaults.GetEncryptor()}
}

// IsEnabled reports whether provider config is usable (encryption configured).
func (s *Store) IsEnabled() bool { return s.encryptor.IsEnabled() }

// Create inserts a new provider config. config may be nil/empty for providers (like mock)
// that need none.
func (s *Store) Create(ctx context.Context, name, providerType string, config json.RawMessage, enabled bool) (*Record, error) {
	if !s.IsEnabled() {
		return nil, ErrProvidersDisabled
	}
	if !validProviderName.MatchString(name) {
		return nil, fmt.Errorf("invalid provider name %q: must start with a letter or digit; only alphanumeric, hyphen, underscore (max 64)", name)
	}
	if !IsRegistered(providerType) {
		return nil, fmt.Errorf("unknown provider type %q (known: %v)", providerType, Types())
	}
	if len(config) == 0 {
		config = json.RawMessage("{}")
	}
	if !json.Valid(config) {
		return nil, fmt.Errorf("config is not valid JSON")
	}

	enc, err := s.encryptor.Encrypt(config)
	if err != nil {
		return nil, fmt.Errorf("failed to encrypt provider config: %w", err)
	}

	now := time.Now()
	rec := &Record{Name: name, ProviderType: providerType, Config: config, Enabled: enabled, CreatedAt: now, UpdatedAt: now}

	if s.db.DriverName() == "sqlite" {
		res, err := s.db.ExecContext(ctx,
			`INSERT INTO autoscale_providers (name, provider_type, config, enabled, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?)`,
			name, providerType, enc, enabled, now, now)
		if err != nil {
			return nil, fmt.Errorf("failed to create provider: %w", err)
		}
		rec.ID, _ = res.LastInsertId()
	} else {
		err := s.db.QueryRowContext(ctx,
			`INSERT INTO autoscale_providers (name, provider_type, config, enabled, created_at, updated_at) VALUES ($1, $2, $3, $4, $5, $6) RETURNING id`,
			name, providerType, enc, enabled, now, now).Scan(&rec.ID)
		if err != nil {
			return nil, fmt.Errorf("failed to create provider: %w", err)
		}
	}
	return rec, nil
}

// providerRow is the raw DB shape.
type providerRow struct {
	ID           int64        `db:"id"`
	Name         string       `db:"name"`
	ProviderType string       `db:"provider_type"`
	Config       []byte       `db:"config"`
	Enabled      bool         `db:"enabled"`
	CreatedAt    sql.NullTime `db:"created_at"`
	UpdatedAt    sql.NullTime `db:"updated_at"`
}

func (s *Store) toRecord(r *providerRow) (*Record, error) {
	dec, err := s.encryptor.Decrypt(r.Config)
	if err != nil {
		return nil, fmt.Errorf("failed to decrypt provider config for %q: %w", r.Name, err)
	}
	rec := &Record{
		ID:           r.ID,
		Name:         r.Name,
		ProviderType: r.ProviderType,
		Config:       json.RawMessage(dec),
		Enabled:      r.Enabled,
	}
	if r.CreatedAt.Valid {
		rec.CreatedAt = r.CreatedAt.Time
	}
	if r.UpdatedAt.Valid {
		rec.UpdatedAt = r.UpdatedAt.Time
	}
	return rec, nil
}

// Get returns a provider config by name (with decrypted config), or nil if not found.
func (s *Store) Get(ctx context.Context, name string) (*Record, error) {
	if !s.IsEnabled() {
		return nil, ErrProvidersDisabled
	}
	query := s.db.Rebind(`SELECT id, name, provider_type, config, enabled, created_at, updated_at FROM autoscale_providers WHERE name = ?`)
	var row providerRow
	err := s.db.GetContext(ctx, &row, query, name)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get provider: %w", err)
	}
	return s.toRecord(&row)
}

// List returns all provider configs (with decrypted config). Callers exposing these over
// the API must mask with MaskConfig.
func (s *Store) List(ctx context.Context) ([]*Record, error) {
	if !s.IsEnabled() {
		return nil, ErrProvidersDisabled
	}
	var rows []providerRow
	err := s.db.SelectContext(ctx, &rows, `SELECT id, name, provider_type, config, enabled, created_at, updated_at FROM autoscale_providers ORDER BY name`)
	if err != nil {
		return nil, fmt.Errorf("failed to list providers: %w", err)
	}
	out := make([]*Record, 0, len(rows))
	for i := range rows {
		rec, err := s.toRecord(&rows[i])
		if err != nil {
			return nil, err
		}
		out = append(out, rec)
	}
	return out, nil
}

// Update replaces a provider config. Returns ErrProviderNotFound if it doesn't exist.
func (s *Store) Update(ctx context.Context, name, providerType string, config json.RawMessage, enabled bool) (*Record, error) {
	if !s.IsEnabled() {
		return nil, ErrProvidersDisabled
	}
	existing, err := s.Get(ctx, name)
	if err != nil {
		return nil, err
	}
	if existing == nil {
		return nil, ErrProviderNotFound
	}
	if providerType == "" {
		providerType = existing.ProviderType
	}
	if !IsRegistered(providerType) {
		return nil, fmt.Errorf("unknown provider type %q (known: %v)", providerType, Types())
	}
	if len(config) == 0 {
		config = json.RawMessage("{}")
	}
	if !json.Valid(config) {
		return nil, fmt.Errorf("config is not valid JSON")
	}
	enc, err := s.encryptor.Encrypt(config)
	if err != nil {
		return nil, fmt.Errorf("failed to encrypt provider config: %w", err)
	}
	now := time.Now()
	query := s.db.Rebind(`UPDATE autoscale_providers SET provider_type = ?, config = ?, enabled = ?, updated_at = ? WHERE name = ?`)
	if _, err := s.db.ExecContext(ctx, query, providerType, enc, enabled, now, name); err != nil {
		return nil, fmt.Errorf("failed to update provider: %w", err)
	}
	existing.ProviderType = providerType
	existing.Config = config
	existing.Enabled = enabled
	existing.UpdatedAt = now
	return existing, nil
}

// Delete removes a provider config. Returns ErrProviderNotFound if it doesn't exist.
func (s *Store) Delete(ctx context.Context, name string) error {
	if !s.IsEnabled() {
		return ErrProvidersDisabled
	}
	query := s.db.Rebind(`DELETE FROM autoscale_providers WHERE name = ?`)
	res, err := s.db.ExecContext(ctx, query, name)
	if err != nil {
		return fmt.Errorf("failed to delete provider: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrProviderNotFound
	}
	return nil
}

// MaskConfig returns a copy of a provider config JSON object with secret-looking string
// values masked, suitable for API responses. Non-object JSON is returned as-is.
func MaskConfig(config json.RawMessage) json.RawMessage {
	if len(config) == 0 {
		return json.RawMessage("{}")
	}
	var obj map[string]interface{}
	if err := json.Unmarshal(config, &obj); err != nil {
		return json.RawMessage("{}")
	}
	for k, v := range obj {
		if s, ok := v.(string); ok && secretKeyHint.MatchString(k) {
			obj[k] = vaults.MaskSecret(s)
		}
	}
	masked, err := json.Marshal(obj)
	if err != nil {
		return json.RawMessage("{}")
	}
	return masked
}

// NormalizeName lowercases/trims a provider name for lookups (names are case-sensitive in
// storage but we trim surrounding whitespace defensively).
func NormalizeName(name string) string { return strings.TrimSpace(name) }
