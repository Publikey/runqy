package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/Publikey/runqy/autoscale/provider"
	"github.com/Publikey/runqy/models"
	"github.com/gin-gonic/gin"
)

// providerView is the API representation of a provider config with secrets masked.
type providerView struct {
	Name         string          `json:"name"`
	ProviderType string          `json:"provider_type"`
	Config       json.RawMessage `json:"config"`
	Enabled      bool            `json:"enabled"`
	CreatedAt    string          `json:"created_at,omitempty"`
	UpdatedAt    string          `json:"updated_at,omitempty"`
}

func toProviderView(r *provider.Record) providerView {
	v := providerView{
		Name:         r.Name,
		ProviderType: r.ProviderType,
		Config:       provider.MaskConfig(r.Config),
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

// ProviderRequest is the create/update body.
type ProviderRequest struct {
	Name         string          `json:"name"`
	ProviderType string          `json:"provider_type"`
	Config       json.RawMessage `json:"config,omitempty"`
	Enabled      *bool           `json:"enabled,omitempty"`
}

// AutoscaleProviderTypesResponse lists the registered provider types (for UI dropdowns).
type AutoscaleProviderTypesResponse struct {
	Types []string `json:"types"`
}

func providersEnabled(c *gin.Context, store *provider.Store) bool {
	if store.IsEnabled() {
		return true
	}
	c.JSON(http.StatusServiceUnavailable, models.APIErrorResponse{
		Errors: []string{"autoscale providers feature disabled: set RUNQY_VAULT_MASTER_KEY to enable encrypted provider configs"},
	})
	return false
}

// ListProviderTypes returns the registered provider type identifiers.
func ListProviderTypes() gin.HandlerFunc {
	return func(c *gin.Context) {
		c.JSON(http.StatusOK, AutoscaleProviderTypesResponse{Types: provider.Types()})
	}
}

// ListAutoscaleProviders returns all provider configs (secrets masked).
func ListAutoscaleProviders(store *provider.Store) gin.HandlerFunc {
	return func(c *gin.Context) {
		if !providersEnabled(c, store) {
			return
		}
		recs, err := store.List(c.Request.Context())
		if err != nil {
			c.JSON(http.StatusInternalServerError, models.APIErrorResponse{Errors: []string{err.Error()}})
			return
		}
		views := make([]providerView, 0, len(recs))
		for _, r := range recs {
			views = append(views, toProviderView(r))
		}
		c.JSON(http.StatusOK, gin.H{"providers": views, "count": len(views)})
	}
}

// GetAutoscaleProvider returns a single provider config (secrets masked).
func GetAutoscaleProvider(store *provider.Store) gin.HandlerFunc {
	return func(c *gin.Context) {
		if !providersEnabled(c, store) {
			return
		}
		rec, err := store.Get(c.Request.Context(), c.Param("name"))
		if err != nil {
			c.JSON(http.StatusInternalServerError, models.APIErrorResponse{Errors: []string{err.Error()}})
			return
		}
		if rec == nil {
			c.JSON(http.StatusNotFound, models.APIErrorResponse{Errors: []string{"provider not found"}})
			return
		}
		c.JSON(http.StatusOK, toProviderView(rec))
	}
}

// CreateAutoscaleProvider creates a new provider config.
func CreateAutoscaleProvider(store *provider.Store) gin.HandlerFunc {
	return func(c *gin.Context) {
		if !providersEnabled(c, store) {
			return
		}
		var req ProviderRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, models.APIErrorResponse{Errors: []string{err.Error()}})
			return
		}
		ctx := c.Request.Context()
		if existing, _ := store.Get(ctx, req.Name); existing != nil {
			c.JSON(http.StatusConflict, models.APIErrorResponse{Errors: []string{"provider '" + req.Name + "' already exists"}})
			return
		}
		enabled := true
		if req.Enabled != nil {
			enabled = *req.Enabled
		}
		rec, err := store.Create(ctx, req.Name, req.ProviderType, req.Config, enabled)
		if err != nil {
			c.JSON(http.StatusBadRequest, models.APIErrorResponse{Errors: []string{err.Error()}})
			return
		}
		c.JSON(http.StatusCreated, gin.H{"message": "provider created", "provider": toProviderView(rec)})
	}
}

// UpdateAutoscaleProvider replaces an existing provider config.
func UpdateAutoscaleProvider(store *provider.Store) gin.HandlerFunc {
	return func(c *gin.Context) {
		if !providersEnabled(c, store) {
			return
		}
		var req ProviderRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, models.APIErrorResponse{Errors: []string{err.Error()}})
			return
		}
		enabled := true
		if req.Enabled != nil {
			enabled = *req.Enabled
		}
		rec, err := store.Update(c.Request.Context(), c.Param("name"), req.ProviderType, req.Config, enabled)
		if errors.Is(err, provider.ErrProviderNotFound) {
			c.JSON(http.StatusNotFound, models.APIErrorResponse{Errors: []string{"provider not found"}})
			return
		}
		if err != nil {
			c.JSON(http.StatusBadRequest, models.APIErrorResponse{Errors: []string{err.Error()}})
			return
		}
		c.JSON(http.StatusOK, gin.H{"message": "provider updated", "provider": toProviderView(rec)})
	}
}

// DeleteAutoscaleProvider removes a provider config.
func DeleteAutoscaleProvider(store *provider.Store) gin.HandlerFunc {
	return func(c *gin.Context) {
		if !providersEnabled(c, store) {
			return
		}
		err := store.Delete(c.Request.Context(), c.Param("name"))
		if errors.Is(err, provider.ErrProviderNotFound) {
			c.JSON(http.StatusNotFound, models.APIErrorResponse{Errors: []string{"provider not found"}})
			return
		}
		if err != nil {
			c.JSON(http.StatusInternalServerError, models.APIErrorResponse{Errors: []string{err.Error()}})
			return
		}
		c.JSON(http.StatusOK, gin.H{"message": "provider deleted"})
	}
}
