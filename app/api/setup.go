package api

import (
	"sync"

	"github.com/Publikey/runqy/autoscale"
	"github.com/Publikey/runqy/autoscale/provider"
	"github.com/Publikey/runqy/config"
	queueworker "github.com/Publikey/runqy/queues"
	"github.com/Publikey/runqy/vaults"
	"github.com/gin-gonic/gin"
	"github.com/Publikey/runqy/third_party/asynq"
)

// Global vault store for use by worker handshake (protected by mutex)
var (
	globalVaultStore   *vaults.Store
	globalVaultStoreMu sync.RWMutex
)

// GetVaultStore returns the global vault store (for use by worker handshake)
func GetVaultStore() *vaults.Store {
	globalVaultStoreMu.RLock()
	defer globalVaultStoreMu.RUnlock()
	return globalVaultStore
}

func SetupAPI(r *gin.Engine, qwStore *queueworker.Store, qwConfigDir string, cfg *config.Config, redisOpt asynq.RedisClientOpt) {
	adminKey  := cfg.APIKey
	workerKey := cfg.WorkerAPIKey
	clientKey := cfg.ClientAPIKey

	// CLI API endpoints remain admin-only
	inspector := asynq.NewInspector(redisOpt)
	SetupCLIAPI(r, inspector, qwStore, adminKey)

	// ── Client routes ──────────────────────────────────────────────────────
	// GET /:uuid is public (UUID acts as a capability token)
	// POST /add and /add-batch require client (or admin) key
	router_predict := r.Group("queue")
	router_predict.GET("/:uuid", GetTaskStatus)
	router_predict.Use(AuthorizeRoles(adminKey, workerKey, clientKey, RoleClient))
	router_predict.POST("/add", AddTask(qwConfigDir, qwStore))
	router_predict.POST("/add-batch", AddTaskBatch(qwConfigDir, qwStore))

	// ── Worker routes ──────────────────────────────────────────────────────
	// Worker registration requires worker (or admin) key
	router_worker := r.Group("worker")
	router_worker.Use(AuthorizeRoles(adminKey, workerKey, clientKey, RoleWorker))
	router_worker.POST("/register", WorkerHandshake(qwStore, cfg))

	// ── Admin routes ───────────────────────────────────────────────────────
	// Queue config and worker management — admin only
	router_workers := r.Group("workers")
	router_workers.Use(AuthorizeRoles(adminKey, workerKey, clientKey, RoleAdmin))
	router_workers.GET("/config/:queue_name", GetQueueConfig(qwStore))
	router_workers.GET("", ListWorkers)
	router_workers.GET("/:worker_id", GetWorker)
	router_workers.GET("/queues", ListQueueConfigs(qwStore))
	router_workers.POST("/queues", CreateQueueConfig(qwStore))
	router_workers.DELETE("/queues/:queue_name", DeleteQueueConfig(qwStore))
	router_workers.POST("/queues/:queue_name/restore", RestoreQueueConfig(qwStore))
	router_workers.POST("/reload", ReloadQueueConfigs(qwStore, qwConfigDir))
}

// SetupVaultsAPI sets up the vaults API routes
func SetupVaultsAPI(r *gin.Engine, vaultStore *vaults.Store, apiKey string) {
	globalVaultStoreMu.Lock()
	globalVaultStore = vaultStore
	globalVaultStoreMu.Unlock()

	// Vaults API - all routes require API key authentication
	router_vaults := r.Group("/api/vaults")
	router_vaults.Use(Authorize(apiKey))

	// Vault CRUD
	router_vaults.GET("", ListVaults(vaultStore))
	router_vaults.POST("", CreateVault(vaultStore))
	router_vaults.GET("/:name", GetVault(vaultStore))
	router_vaults.DELETE("/:name", DeleteVault(vaultStore))

	// Vault entry CRUD
	router_vaults.POST("/:name/entries", SetEntry(vaultStore))
	router_vaults.GET("/:name/entries", ListEntries(vaultStore))
	router_vaults.DELETE("/:name/entries/:key", DeleteEntry(vaultStore))
}

// SetupAutoscaleAPI registers the GPU autoscaling routes (admin-only).
func SetupAutoscaleAPI(r *gin.Engine, instanceStore *autoscale.Store, providerStore *provider.Store, apiKey string) {
	grp := r.Group("/api/autoscale")
	grp.Use(Authorize(apiKey))

	// Status + instance protection
	grp.GET("/status", AutoscaleStatus(instanceStore))
	grp.POST("/instances/:id/protect", ProtectInstance(instanceStore))
	grp.POST("/instances/:id/unprotect", UnprotectInstance(instanceStore))

	// Provider config CRUD (vault-independent, encrypted at rest)
	grp.GET("/provider-types", ListProviderTypes())
	grp.GET("/providers", ListAutoscaleProviders(providerStore))
	grp.POST("/providers", CreateAutoscaleProvider(providerStore))
	grp.GET("/providers/:name", GetAutoscaleProvider(providerStore))
	grp.PUT("/providers/:name", UpdateAutoscaleProvider(providerStore))
	grp.DELETE("/providers/:name", DeleteAutoscaleProvider(providerStore))
}
