// Package alerts evaluates queue health against alert rules and notifies by email
// (SMTP or SendGrid).
package alerts

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	queueworker "github.com/Publikey/runqy/queues"
	"github.com/Publikey/runqy/vaults"
	"github.com/jmoiron/sqlx"
)

// Secret fields may hold a literal value, a "vault://vault/key" reference, or be empty to
// fall back to these environment variables.
const (
	EnvSMTPPassword   = "RUNQY_SMTP_PASSWORD"
	EnvSendGridAPIKey = "RUNQY_SENDGRID_API_KEY"
)

const vaultPrefix = "vault://"

// ErrPlainSecret is returned when a literal secret would be stored unencrypted.
var ErrPlainSecret = errors.New("storing a literal SMTP password or SendGrid API key requires RUNQY_VAULT_MASTER_KEY; use a vault:// reference or the " + EnvSMTPPassword + " / " + EnvSendGridAPIKey + " environment variable instead")

// SettingsStore persists the global alert settings (table alert_settings, single row).
type SettingsStore struct {
	db        *sqlx.DB
	encryptor *vaults.Encryptor
}

// NewSettingsStore creates a store sharing the global vault encryptor.
func NewSettingsStore(db *sqlx.DB) *SettingsStore {
	return &SettingsStore{db: db, encryptor: vaults.GetEncryptor()}
}

// Get returns the stored settings, or nil when none were ever saved.
func (s *SettingsStore) Get(ctx context.Context) (*queueworker.AlertSettings, error) {
	var row struct {
		Config    []byte `db:"config"`
		Encrypted bool   `db:"encrypted"`
	}
	err := s.db.GetContext(ctx, &row, `SELECT config, encrypted FROM alert_settings WHERE id = 1`)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to load alert settings: %w", err)
	}
	data := row.Config
	if row.Encrypted {
		if !s.encryptor.IsEnabled() {
			return nil, fmt.Errorf("alert settings are encrypted but RUNQY_VAULT_MASTER_KEY is not set")
		}
		if data, err = s.encryptor.Decrypt(row.Config); err != nil {
			return nil, fmt.Errorf("failed to decrypt alert settings: %w", err)
		}
	}
	var settings queueworker.AlertSettings
	if err := json.Unmarshal(data, &settings); err != nil {
		return nil, fmt.Errorf("failed to decode alert settings: %w", err)
	}
	return &settings, nil
}

// Save validates and stores the settings, encrypted when the vault master key is set.
// Without it, literal secrets are refused (vault:// references and env fallbacks are fine).
func (s *SettingsStore) Save(ctx context.Context, settings *queueworker.AlertSettings) error {
	if settings == nil {
		return fmt.Errorf("alert settings are required")
	}
	if err := settings.Validate(); err != nil {
		return err
	}
	data, err := json.Marshal(settings)
	if err != nil {
		return err
	}
	encrypted := s.encryptor.IsEnabled()
	if encrypted {
		if data, err = s.encryptor.Encrypt(data); err != nil {
			return fmt.Errorf("failed to encrypt alert settings: %w", err)
		}
	} else if hasLiteralSecret(settings) {
		return ErrPlainSecret
	}

	now := time.Now()
	query := s.db.Rebind(`UPDATE alert_settings SET config = ?, encrypted = ?, updated_at = ? WHERE id = 1`)
	res, err := s.db.ExecContext(ctx, query, data, encrypted, now)
	if err != nil {
		return fmt.Errorf("failed to save alert settings: %w", err)
	}
	if n, _ := res.RowsAffected(); n > 0 {
		return nil
	}
	query = s.db.Rebind(`INSERT INTO alert_settings (id, config, encrypted, updated_at) VALUES (1, ?, ?, ?)`)
	if _, err := s.db.ExecContext(ctx, query, data, encrypted, now); err != nil {
		return fmt.Errorf("failed to save alert settings: %w", err)
	}
	return nil
}

func isLiteralSecret(v string) bool {
	return v != "" && !strings.HasPrefix(v, vaultPrefix)
}

func hasLiteralSecret(s *queueworker.AlertSettings) bool {
	return (s.SMTP != nil && isLiteralSecret(s.SMTP.Password)) || (s.SendGrid != nil && isLiteralSecret(s.SendGrid.APIKey))
}

// MaskSecrets returns a copy safe for API responses: literal secrets are masked, vault
// references are shown as-is (they are not secret).
func MaskSecrets(s *queueworker.AlertSettings) *queueworker.AlertSettings {
	if s == nil {
		return nil
	}
	out := *s
	if s.SMTP != nil {
		smtp := *s.SMTP
		if isLiteralSecret(smtp.Password) {
			smtp.Password = vaults.MaskSecret(smtp.Password)
		}
		out.SMTP = &smtp
	}
	if s.SendGrid != nil {
		sg := *s.SendGrid
		if isLiteralSecret(sg.APIKey) {
			sg.APIKey = vaults.MaskSecret(sg.APIKey)
		}
		out.SendGrid = &sg
	}
	return &out
}

// RestoreMaskedSecrets puts back stored secrets that a client sent back in masked form
// (an edit form pre-filled from MaskSecrets), so saving does not overwrite them.
func RestoreMaskedSecrets(incoming, stored *queueworker.AlertSettings) {
	if incoming == nil || stored == nil {
		return
	}
	if incoming.SMTP != nil && stored.SMTP != nil && isLiteralSecret(stored.SMTP.Password) &&
		incoming.SMTP.Password == vaults.MaskSecret(stored.SMTP.Password) {
		incoming.SMTP.Password = stored.SMTP.Password
	}
	if incoming.SendGrid != nil && stored.SendGrid != nil && isLiteralSecret(stored.SendGrid.APIKey) &&
		incoming.SendGrid.APIKey == vaults.MaskSecret(stored.SendGrid.APIKey) {
		incoming.SendGrid.APIKey = stored.SendGrid.APIKey
	}
}

// VaultReader reads a vault entry (vaults.Store satisfies it).
type VaultReader interface {
	GetEntry(ctx context.Context, vaultName, key string) (string, bool, error)
}

// ResolveSecret turns a configured secret into its value: a vault:// reference is read
// from the vault, an empty value falls back to the environment variable envVar.
func ResolveSecret(ctx context.Context, value, envVar string, vault VaultReader, getenv func(string) string) (string, error) {
	switch {
	case value == "":
		return getenv(envVar), nil
	case strings.HasPrefix(value, vaultPrefix):
		ref := strings.TrimPrefix(value, vaultPrefix)
		i := strings.Index(ref, "/")
		if i <= 0 || i == len(ref)-1 {
			return "", fmt.Errorf("invalid vault reference %q (expected vault://vault-name/key)", value)
		}
		if vault == nil {
			return "", fmt.Errorf("vault reference %q cannot be resolved: vaults are not enabled", value)
		}
		v, _, err := vault.GetEntry(ctx, ref[:i], ref[i+1:])
		if err != nil {
			return "", fmt.Errorf("vault reference %q: %w", value, err)
		}
		return v, nil
	default:
		return value, nil
	}
}
