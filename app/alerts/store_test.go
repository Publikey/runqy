package alerts

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Publikey/runqy/models"
	queueworker "github.com/Publikey/runqy/queues"
	"github.com/Publikey/runqy/vaults"
	"github.com/jmoiron/sqlx"
	_ "modernc.org/sqlite"
)

func newTestSettingsStore(t *testing.T, enc *vaults.Encryptor) *SettingsStore {
	t.Helper()
	db, err := sqlx.Connect("sqlite", filepath.Join(t.TempDir(), "alerts.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	if err := models.EnsureSchema(db, false); err != nil {
		t.Fatal(err)
	}
	return &SettingsStore{db: db, encryptor: enc}
}

func testEncryptor(t *testing.T) *vaults.Encryptor {
	t.Helper()
	key, err := vaults.GenerateKey()
	if err != nil {
		t.Fatal(err)
	}
	enc, _ := vaults.InitEncryptor(key)
	if !enc.IsEnabled() {
		t.Fatal("encryptor not enabled")
	}
	return enc
}

func TestSettingsStoreRoundTripEncrypted(t *testing.T) {
	s := newTestSettingsStore(t, testEncryptor(t))
	ctx := context.Background()

	if got, err := s.Get(ctx); err != nil || got != nil {
		t.Fatalf("empty store: %v %v", got, err)
	}
	in := baseSettings(queueworker.AlertRule{Type: queueworker.AlertNoWorkers, For: "2m"})
	in.SMTP.Password = "hunter22"
	if err := s.Save(ctx, in); err != nil {
		t.Fatal(err)
	}
	in.Recipients = []string{"other@example.com"} // second save updates the single row
	if err := s.Save(ctx, in); err != nil {
		t.Fatal(err)
	}
	got, err := s.Get(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if got.SMTP.Password != "hunter22" || got.Recipients[0] != "other@example.com" || got.Rules[0].For != "2m" {
		t.Fatalf("round trip mismatch: %+v", got)
	}

	var raw []byte
	if err := s.db.Get(&raw, `SELECT config FROM alert_settings WHERE id = 1`); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "hunter22") {
		t.Error("password stored in clear")
	}
}

func TestSettingsStoreWithoutEncryption(t *testing.T) {
	s := newTestSettingsStore(t, &vaults.Encryptor{})
	ctx := context.Background()

	literal := baseSettings()
	literal.SMTP.Password = "hunter22"
	if err := s.Save(ctx, literal); !errors.Is(err, ErrPlainSecret) {
		t.Fatalf("literal secret without master key: err = %v, want ErrPlainSecret", err)
	}

	ref := baseSettings()
	ref.SMTP.Password = "vault://mail/pw" // references and env fallback are fine
	if err := s.Save(ctx, ref); err != nil {
		t.Fatal(err)
	}
	got, _ := s.Get(ctx)
	if got.SMTP.Password != "vault://mail/pw" {
		t.Fatalf("got %+v", got.SMTP)
	}

	invalid := baseSettings()
	invalid.Recipients = nil
	if err := s.Save(ctx, invalid); err == nil {
		t.Fatal("invalid settings saved")
	}
}

func TestMaskAndRestoreSecrets(t *testing.T) {
	stored := baseSettings()
	stored.SMTP.Password = "hunter22"
	stored.SendGrid = &queueworker.SendGridSettings{APIKey: "vault://mail/sg"}

	masked := MaskSecrets(stored)
	if masked.SMTP.Password == "hunter22" || stored.SMTP.Password != "hunter22" {
		t.Fatalf("mask leaked or mutated the original: %q / %q", masked.SMTP.Password, stored.SMTP.Password)
	}
	if masked.SendGrid.APIKey != "vault://mail/sg" {
		t.Error("vault reference should not be masked")
	}

	RestoreMaskedSecrets(masked, stored)
	if masked.SMTP.Password != "hunter22" {
		t.Error("masked password not restored")
	}
	changed := MaskSecrets(stored)
	changed.SMTP.Password = "new-password"
	RestoreMaskedSecrets(changed, stored)
	if changed.SMTP.Password != "new-password" {
		t.Error("a new password was overwritten")
	}
}
