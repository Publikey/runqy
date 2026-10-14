package alerts

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	queueworker "github.com/Publikey/runqy/queues"
)

func doJSON(t *testing.T, h http.HandlerFunc, method, body string) (*httptest.ResponseRecorder, map[string]json.RawMessage) {
	t.Helper()
	req := httptest.NewRequest(method, "/alerts/settings", strings.NewReader(body))
	rec := httptest.NewRecorder()
	h(rec, req)
	var out map[string]json.RawMessage
	_ = json.Unmarshal(rec.Body.Bytes(), &out)
	return rec, out
}

func TestSettingsEndpoints(t *testing.T) {
	a := &API{Store: newTestSettingsStore(t, testEncryptor(t))}

	// Nothing saved yet: a disabled default comes back.
	rec, out := doJSON(t, a.GetSettings, http.MethodGet, "")
	if rec.Code != http.StatusOK || !strings.Contains(string(out["settings"]), `"enabled":false`) {
		t.Fatalf("GET empty: %d %s", rec.Code, rec.Body)
	}

	body := `{"enabled":true,"provider":"smtp","from":"a@example.com","recipients":["ops@example.com"],
		"smtp":{"host":"smtp.example.com","username":"u","password":"hunter22"},
		"rules":[{"type":"no_workers","for":"2m"}]}`
	rec, out = doJSON(t, a.PutSettings, http.MethodPut, body)
	if rec.Code != http.StatusOK {
		t.Fatalf("PUT: %d %s", rec.Code, rec.Body)
	}
	if strings.Contains(string(out["settings"]), "hunter22") {
		t.Fatal("PUT response leaks the password")
	}

	// Save the form as the UI would: masked password sent back unchanged.
	var masked queueworker.AlertSettings
	_, out = doJSON(t, a.GetSettings, http.MethodGet, "")
	_ = json.Unmarshal(out["settings"], &masked)
	masked.Recipients = []string{"team@example.com"}
	b, _ := json.Marshal(masked)
	if rec, _ = doJSON(t, a.PutSettings, http.MethodPut, string(b)); rec.Code != http.StatusOK {
		t.Fatalf("PUT masked: %d %s", rec.Code, rec.Body)
	}
	stored, _ := a.Store.Get(httptest.NewRequest("GET", "/", nil).Context())
	if stored.SMTP.Password != "hunter22" || stored.Recipients[0] != "team@example.com" {
		t.Fatalf("masked password overwrote the stored one: %+v", stored.SMTP)
	}

	// Invalid settings are rejected with the error format of the rest of the API.
	rec, out = doJSON(t, a.PutSettings, http.MethodPut, `{"enabled":true,"provider":"smtp"}`)
	if rec.Code != http.StatusBadRequest || out["errors"] == nil {
		t.Fatalf("invalid PUT: %d %s", rec.Code, rec.Body)
	}
	if rec, _ = doJSON(t, a.PutSettings, http.MethodPut, `not json`); rec.Code != http.StatusBadRequest {
		t.Fatalf("bad JSON: %d", rec.Code)
	}
}

func TestTestEndpointReportsDeliveryErrors(t *testing.T) {
	a := &API{Store: newTestSettingsStore(t, testEncryptor(t))}
	rec, _ := doJSON(t, a.Test, http.MethodPost, "")
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("test without settings: %d %s", rec.Code, rec.Body)
	}
	// SMTP on a closed port: the delivery error is surfaced.
	body := `{"enabled":true,"provider":"smtp","from":"a@example.com","recipients":["ops@example.com"],
		"smtp":{"host":"127.0.0.1","port":1,"tls":"none"}}`
	rec, out := doJSON(t, a.Test, http.MethodPost, body)
	if rec.Code != http.StatusBadGateway || !strings.Contains(string(out["errors"]), "test email failed") {
		t.Fatalf("unreachable SMTP: %d %s", rec.Code, rec.Body)
	}
}
