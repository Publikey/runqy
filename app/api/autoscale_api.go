package api

import (
	"errors"
	"net/http"

	"github.com/Publikey/runqy/autoscale"
	"github.com/Publikey/runqy/models"
	"github.com/gin-gonic/gin"
)

// AutoscaleStatusResponse is the payload for GET /api/autoscale/status.
type AutoscaleStatusResponse struct {
	Instances []*autoscale.Instance `json:"instances"`
	Count     int                   `json:"count"`
	TotalCost float64               `json:"total_cost"`
}

// AutoscaleStatus returns the current state of all tracked autoscaler instances.
// @Summary Autoscaling status
// @Description Returns tracked GPU instances, their states, and accumulated cost
// @Tags autoscale
// @Produce json
// @Success 200 {object} AutoscaleStatusResponse
// @Router /api/autoscale/status [get]
func AutoscaleStatus(store *autoscale.Store) gin.HandlerFunc {
	return func(c *gin.Context) {
		instances, err := store.ListAll(c.Request.Context())
		if err != nil {
			c.JSON(http.StatusInternalServerError, models.APIErrorResponse{Errors: []string{err.Error()}})
			return
		}
		var total float64
		for _, inst := range instances {
			total += inst.CostAccumulated
		}
		c.JSON(http.StatusOK, AutoscaleStatusResponse{
			Instances: instances,
			Count:     len(instances),
			TotalCost: total,
		})
	}
}

// ProtectInstance marks a managed instance as protected from scale-down.
// @Summary Protect an autoscaler instance
// @Tags autoscale
// @Produce json
// @Param id path string true "Instance ID"
// @Success 200 {object} map[string]string
// @Router /api/autoscale/instances/{id}/protect [post]
func ProtectInstance(store *autoscale.Store) gin.HandlerFunc {
	return setProtected(store, true)
}

// UnprotectInstance clears the protection flag on a managed instance.
// @Summary Unprotect an autoscaler instance
// @Tags autoscale
// @Produce json
// @Param id path string true "Instance ID"
// @Success 200 {object} map[string]string
// @Router /api/autoscale/instances/{id}/unprotect [post]
func UnprotectInstance(store *autoscale.Store) gin.HandlerFunc {
	return setProtected(store, false)
}

func setProtected(store *autoscale.Store, protected bool) gin.HandlerFunc {
	return func(c *gin.Context) {
		id := c.Param("id")
		if id == "" {
			c.JSON(http.StatusBadRequest, models.APIErrorResponse{Errors: []string{"instance id is required"}})
			return
		}
		err := store.SetProtected(c.Request.Context(), id, protected)
		if errors.Is(err, autoscale.ErrInstanceNotFound) {
			c.JSON(http.StatusNotFound, models.APIErrorResponse{Errors: []string{"instance not found"}})
			return
		}
		if err != nil {
			c.JSON(http.StatusInternalServerError, models.APIErrorResponse{Errors: []string{err.Error()}})
			return
		}
		state := "unprotected"
		if protected {
			state = "protected"
		}
		c.JSON(http.StatusOK, gin.H{"message": "instance " + state, "instance_id": id, "protected": protected})
	}
}
