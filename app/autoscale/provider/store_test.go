package provider

import (
	"context"
	"encoding/json"
	"path/filepath"
	"testing"

	"github.com/Publikey/runqy/models"
	"github.com/Publikey/runqy/vaults"
	"github.com/jmoiron/sqlx"
	_ "modernc.org/sqlite"
)

func newTestStore(t *testing.T) *Store {
	t.Helper()
	key, err := vaults.GenerateKey()
	if err != nil {
		t.Fatal(err)
	}
	enc, _ := vaults.InitEncryptor(key)
	if !enc.IsEnabled() {
		t.Fatal("encryptor not enabled")
	}
	db, err := sqlx.Connect("sqlite", filepath.Join(t.TempDir(), "providers.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	if err := models.EnsureSchema(db, false); err != nil {
		t.Fatal(err)
	}
	return &Store{db: db, encryptor: enc}
}

func configMap(t *testing.T, raw json.RawMessage) map[string]interface{} {
	t.Helper()
	var m map[string]interface{}
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatal(err)
	}
	return m
}

func TestStoreUpdateKeepsSecrets(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	if _, err := s.Create(ctx, "gpu", "mock", json.RawMessage(`{"api_key":"sk-live-123456","region":"eu"}`), true); err != nil {
		t.Fatal(err)
	}

	// Edit form pre-filled from the API: the key comes back masked, region changes.
	masked := configMap(t, MaskConfig(json.RawMessage(`{"api_key":"sk-live-123456"}`)))["api_key"].(string)
	in, _ := json.Marshal(map[string]string{"api_key": masked, "region": "us"})
	rec, err := s.Update(ctx, "gpu", "", in, nil)
	if err != nil {
		t.Fatal(err)
	}
	got := configMap(t, rec.Config)
	if got["api_key"] != "sk-live-123456" || got["region"] != "us" {
		t.Fatalf("masked secret not restored: %v", got)
	}
	if !rec.Enabled {
		t.Fatal("enabled flag changed although not provided")
	}

	// Disable only: config must be untouched.
	off := false
	rec, err = s.Update(ctx, "gpu", "", nil, &off)
	if err != nil {
		t.Fatal(err)
	}
	stored, _ := s.Get(ctx, "gpu")
	if stored.Enabled || configMap(t, stored.Config)["api_key"] != "sk-live-123456" {
		t.Fatalf("disable-only update clobbered state: enabled=%v config=%s", stored.Enabled, stored.Config)
	}

	// A genuinely new secret replaces the old one.
	if _, err := s.Update(ctx, "gpu", "", json.RawMessage(`{"api_key":"sk-new-999999"}`), nil); err != nil {
		t.Fatal(err)
	}
	stored, _ = s.Get(ctx, "gpu")
	if got := configMap(t, stored.Config); got["api_key"] != "sk-new-999999" || got["region"] != nil {
		t.Fatalf("new config not applied as a replacement: %v", got)
	}
}
