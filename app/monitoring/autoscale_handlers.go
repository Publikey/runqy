package monitoring

import (
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"time"

	"github.com/Publikey/runqy/autoscale"
	asprovider "github.com/Publikey/runqy/autoscale/provider"
	"github.com/gorilla/mux"
)

// AutoscaleStatusResponse mirrors the main API's GET /api/autoscale/status payload.
type AutoscaleStatusResponse struct {
	Instances []*autoscale.Instance `json:"instances"`
	Count     int                   `json:"count"`
	TotalCost float64               `json:"total_cost"`
}

// autoscaleProviderView is a provider config with secrets masked, mirroring the main API.
type autoscaleProviderView struct {
	Name         string          `json:"name"`
	ProviderType string          `json:"provider_type"`
	Config       json.RawMessage `json:"config"`
	Enabled      bool            `json:"enabled"`
	CreatedAt    string          `json:"created_at,omitempty"`
	UpdatedAt    string          `json:"updated_at,omitempty"`
}

func toAutoscaleProviderView(r *asprovider.Record) autoscaleProviderView {
	v := autoscaleProviderView{
		Name:         r.Name,
		ProviderType: r.ProviderType,
		Config:       asprovider.MaskConfig(r.Config),
		Enabled:      r.Enabled,
	}
	if !r.CreatedAt.IsZero() {
		v.CreatedAt = r.CreatedAt.Format(time.RFC3339)
	}
	if !r.UpdatedAt.IsZero() {
		v.UpdatedAt = r.UpdatedAt.Format(time.RFC3339)
	}
	return v
}

// autoscaleProviderRequest is the create/update body for a provider config.
type autoscaleProviderRequest struct {
	Name         string          `json:"name"`
	ProviderType string          `json:"provider_type"`
	Config       json.RawMessage `json:"config,omitempty"`
	Enabled      *bool           `json:"enabled,omitempty"`
}

func checkAutoscaleProvidersEnabled(w http.ResponseWriter, store *asprovider.Store) bool {
	if store.IsEnabled() {
		return true
	}
	writeJSONError(w, "autoscale providers feature disabled: set RUNQY_VAULT_MASTER_KEY to enable encrypted provider configs", http.StatusServiceUnavailable)
	return false
}

func newAutoscaleStatusHandlerFunc(store *autoscale.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		instances, err := store.ListAll(r.Context())
		if err != nil {
			writeJSONError(w, err.Error(), http.StatusInternalServerError)
			return
		}
		var total float64
		for _, inst := range instances {
			total += inst.CostAccumulated
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(AutoscaleStatusResponse{
			Instances: instances,
			Count:     len(instances),
			TotalCost: total,
		})
	}
}

func newAutoscaleSetProtectedHandlerFunc(store *autoscale.Store, protected bool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := mux.Vars(r)["id"]
		if id == "" {
			writeJSONError(w, "instance id is required", http.StatusBadRequest)
			return
		}
		err := store.SetProtected(r.Context(), id, protected)
		if errors.Is(err, autoscale.ErrInstanceNotFound) {
			writeJSONError(w, "instance not found", http.StatusNotFound)
			return
		}
		if err != nil {
			writeJSONError(w, err.Error(), http.StatusInternalServerError)
			return
		}
		state := "unprotected"
		if protected {
			state = "protected"
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"message":     "instance " + state,
			"instance_id": id,
			"protected":   protected,
		})
	}
}

func newAutoscaleProviderTypesHandlerFunc() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{"types": asprovider.Types()})
	}
}

func newListAutoscaleProvidersHandlerFunc(store *asprovider.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !checkAutoscaleProvidersEnabled(w, store) {
			return
		}
		recs, err := store.List(r.Context())
		if err != nil {
			writeJSONError(w, err.Error(), http.StatusInternalServerError)
			return
		}
		views := make([]autoscaleProviderView, 0, len(recs))
		for _, rec := range recs {
			views = append(views, toAutoscaleProviderView(rec))
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{"providers": views, "count": len(views)})
	}
}

func newGetAutoscaleProviderHandlerFunc(store *asprovider.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !checkAutoscaleProvidersEnabled(w, store) {
			return
		}
		rec, err := store.Get(r.Context(), mux.Vars(r)["name"])
		if err != nil {
			writeJSONError(w, err.Error(), http.StatusInternalServerError)
			return
		}
		if rec == nil {
			writeJSONError(w, "provider not found", http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(toAutoscaleProviderView(rec))
	}
}

func newCreateAutoscaleProviderHandlerFunc(store *asprovider.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !checkAutoscaleProvidersEnabled(w, store) {
			return
		}
		var req autoscaleProviderRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeJSONError(w, "invalid request body", http.StatusBadRequest)
			return
		}
		ctx := r.Context()
		if existing, _ := store.Get(ctx, req.Name); existing != nil {
			writeJSONError(w, "provider '"+req.Name+"' already exists", http.StatusConflict)
			return
		}
		enabled := true
		if req.Enabled != nil {
			enabled = *req.Enabled
		}
		rec, err := store.Create(ctx, req.Name, req.ProviderType, req.Config, enabled)
		if err != nil {
			writeJSONError(w, err.Error(), http.StatusBadRequest)
			return
		}
		log.Printf("[AUTOSCALE] Created provider: %s (%s)", rec.Name, rec.ProviderType)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"message":  "provider created",
			"provider": toAutoscaleProviderView(rec),
		})
	}
}

func newUpdateAutoscaleProviderHandlerFunc(store *asprovider.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !checkAutoscaleProvidersEnabled(w, store) {
			return
		}
		var req autoscaleProviderRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeJSONError(w, "invalid request body", http.StatusBadRequest)
			return
		}
		enabled := true
		if req.Enabled != nil {
			enabled = *req.Enabled
		}
		rec, err := store.Update(r.Context(), mux.Vars(r)["name"], req.ProviderType, req.Config, enabled)
		if errors.Is(err, asprovider.ErrProviderNotFound) {
			writeJSONError(w, "provider not found", http.StatusNotFound)
			return
		}
		if err != nil {
			writeJSONError(w, err.Error(), http.StatusBadRequest)
			return
		}
		log.Printf("[AUTOSCALE] Updated provider: %s (%s)", rec.Name, rec.ProviderType)
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"message":  "provider updated",
			"provider": toAutoscaleProviderView(rec),
		})
	}
}

func newDeleteAutoscaleProviderHandlerFunc(store *asprovider.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !checkAutoscaleProvidersEnabled(w, store) {
			return
		}
		name := mux.Vars(r)["name"]
		err := store.Delete(r.Context(), name)
		if errors.Is(err, asprovider.ErrProviderNotFound) {
			writeJSONError(w, "provider not found", http.StatusNotFound)
			return
		}
		if err != nil {
			writeJSONError(w, err.Error(), http.StatusInternalServerError)
			return
		}
		log.Printf("[AUTOSCALE] Deleted provider: %s", name)
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"message": "provider deleted"})
	}
}
