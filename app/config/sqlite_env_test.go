package config

import "testing"

func TestUseSQLiteFromEnv(t *testing.T) {
	t.Setenv("RUNQY_USE_SQLITE", "")
	if Load().UseSQLite {
		t.Fatal("SQLite enabled without RUNQY_USE_SQLITE")
	}
	t.Setenv("RUNQY_USE_SQLITE", "TRUE")
	t.Setenv("SQLITE_DB_PATH", "/data/runqy.db")
	cfg := Load()
	if !cfg.UseSQLite || cfg.SQLiteDBPath != "/data/runqy.db" {
		t.Fatalf("got UseSQLite=%v path=%q", cfg.UseSQLite, cfg.SQLiteDBPath)
	}
}
