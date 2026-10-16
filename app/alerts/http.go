package alerts

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strconv"
	"time"

	queueworker "github.com/Publikey/runqy/queues"
	"github.com/redis/go-redis/v9"
)

// API serves the alerting endpoints. It is plain net/http so both the REST API (gin)
// and the dashboard API (gorilla/mux) mount the same handlers:
//
//	GET  /alerts            active alerts, recent history, evaluator status
//	GET  /alerts/settings   global settings (secrets masked)
//	PUT  /alerts/settings   save global settings
//	POST /alerts/test       send a test email (body: settings to test, or empty for the saved ones)
type API struct {
	Store     *SettingsStore
	Redis     *redis.Client
	Vault     VaultReader // may be nil
	PublicURL string
}

// OverviewResponse is the GET /alerts payload.
type OverviewResponse struct {
	Enabled bool           `json:"enabled"`
	Active  []ActiveAlert  `json:"active"`
	History []HistoryEntry `json:"history"`
	Status  Status         `json:"status"`
}

type errorsBody struct {
	Errors []string `json:"errors"`
}

func writeJSON(w http.ResponseWriter, code int, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, code int, msg string) {
	writeJSON(w, code, errorsBody{Errors: []string{msg}})
}

// Overview handles GET /alerts. Query: history=<n> (default 50).
func (a *API) Overview(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	limit := int64(50)
	if n, err := strconv.ParseInt(r.URL.Query().Get("history"), 10, 64); err == nil && n > 0 {
		limit = n
	}
	settings, err := a.Store.Get(ctx)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	active, err := Active(ctx, a.Redis)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	history, err := History(ctx, a.Redis, limit)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	status, _ := GetStatus(ctx, a.Redis)
	writeJSON(w, http.StatusOK, OverviewResponse{
		Enabled: settings != nil && settings.Enabled,
		Active:  active,
		History: history,
		Status:  status,
	})
}

// GetSettings handles GET /alerts/settings.
func (a *API) GetSettings(w http.ResponseWriter, r *http.Request) {
	settings, err := a.Store.Get(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if settings == nil {
		settings = &queueworker.AlertSettings{Provider: queueworker.AlertProviderSMTP}
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"settings": MaskSecrets(settings)})
}

// PutSettings handles PUT /alerts/settings.
func (a *API) PutSettings(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	incoming, ok := a.decodeSettings(w, r)
	if !ok || incoming == nil {
		if ok {
			writeError(w, http.StatusBadRequest, "request body is required")
		}
		return
	}
	stored, err := a.Store.Get(ctx)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	RestoreMaskedSecrets(incoming, stored)
	if err := a.Store.Save(ctx, incoming); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"message": "alert settings saved", "settings": MaskSecrets(incoming)})
}

// Test handles POST /alerts/test.
func (a *API) Test(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	incoming, ok := a.decodeSettings(w, r)
	if !ok {
		return
	}
	stored, err := a.Store.Get(ctx)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	settings := stored
	if incoming != nil {
		RestoreMaskedSecrets(incoming, stored)
		settings = incoming
	}
	if settings == nil {
		writeError(w, http.StatusBadRequest, "alerting is not configured")
		return
	}
	tctx, cancel := context.WithTimeout(ctx, 45*time.Second)
	defer cancel()
	if err := SendTest(tctx, settings, a.Vault, a.PublicURL); err != nil {
		writeError(w, http.StatusBadGateway, "test email failed: "+err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"message": "test email sent"})
}

// decodeSettings reads an optional settings body (nil when empty).
func (a *API) decodeSettings(w http.ResponseWriter, r *http.Request) (*queueworker.AlertSettings, bool) {
	body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	if err != nil {
		writeError(w, http.StatusBadRequest, "cannot read request body")
		return nil, false
	}
	if len(body) == 0 {
		return nil, true
	}
	var s queueworker.AlertSettings
	if err := json.Unmarshal(body, &s); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return nil, false
	}
	return &s, true
}
