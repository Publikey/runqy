package cmd

import (
	"testing"
)

// withTempHome points the credentials file at a temporary home directory.
func withTempHome(t *testing.T) {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	t.Setenv("USERPROFILE", dir) // os.UserHomeDir on Windows
}

func login(t *testing.T, name, server string, noSwitch bool) {
	t.Helper()
	loginServer, loginAPIKey, loginProfile, loginNoSwitch = server, "key-"+name, name, noSwitch
	t.Cleanup(func() { loginServer, loginAPIKey, loginProfile, loginNoSwitch = "", "", "default", false })
	if err := runLogin(nil, nil); err != nil {
		t.Fatalf("login %s: %v", name, err)
	}
}

func currentProfile(t *testing.T) string {
	t.Helper()
	name, err := GetCurrentProfileName()
	if err != nil {
		t.Fatal(err)
	}
	return name
}

func TestLoginActivatesTheNewProfile(t *testing.T) {
	withTempHome(t)

	login(t, "default", "https://prod.example.com", false)
	if got := currentProfile(t); got != "default" {
		t.Fatalf("after first login: current = %q, want default", got)
	}

	// Logging into a second, named profile must make it active; it used to stay on
	// "default" and send the next commands to production.
	login(t, "staging", "https://staging.example.com/", false)
	if got := currentProfile(t); got != "staging" {
		t.Fatalf("after login -n staging: current = %q, want staging", got)
	}
	creds, err := LoadCredentials("staging")
	if err != nil || creds.URL != "https://staging.example.com" {
		t.Fatalf("staging profile = %+v, %v (trailing slash should be trimmed)", creds, err)
	}
}

func TestLoginNoSwitchKeepsTheActiveProfile(t *testing.T) {
	withTempHome(t)

	login(t, "default", "https://prod.example.com", false)
	login(t, "staging", "https://staging.example.com", true)
	if got := currentProfile(t); got != "default" {
		t.Fatalf("with --no-switch: current = %q, want default", got)
	}
	if _, err := LoadCredentials("staging"); err != nil {
		t.Fatalf("staging profile not saved: %v", err)
	}
}
