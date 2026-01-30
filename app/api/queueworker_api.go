package api

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/Publikey/runqy/config"
	queueworker "github.com/Publikey/runqy/queues"
	"github.com/Publikey/runqy/vaults"
	"github.com/gin-gonic/gin"
)

// HandshakeRequest is the worker handshake request body
type HandshakeRequest struct {
	Queue    string `json:"queue"`
	Hostname string `json:"hostname"`
	OS       string `json:"os"`
	Version  string `json:"version"`
}

// WorkerRedisConfig contains Redis connection info for the worker
type WorkerRedisConfig struct {
	Addr     string `json:"addr"`
	Password string `json:"password"`
	TLS      bool   `json:"use_tls"`
	DB       int    `json:"db"`
}

// WorkerQueueConfig contains queue info for the worker
type WorkerQueueConfig struct {
	Name     string `json:"name"`
	Priority int    `json:"priority"`
}

// SubQueueConfig contains sub-queue info for the worker
type SubQueueConfig struct {
	Name     string `json:"name"`
	Priority int    `json:"priority"`
}

// WorkerDeploymentConfig contains deployment info for the worker
type WorkerDeploymentConfig struct {
	GitURL             string   `json:"git_url"`
	Branch             string   `json:"branch"`
	CodePath           string   `json:"code_path,omitempty"`
	StartupCmd         string   `json:"startup_cmd"`
	Mode               string   `json:"mode,omitempty"`
	StartupTimeoutSecs int      `json:"startup_timeout_secs"`
	RedisStorage       bool     `json:"redis_storage"`
	Vaults             []string `json:"vaults,omitempty"`
}

// WorkerConfigResponse is the full response for worker registration
type WorkerConfigResponse struct {
	Redis      WorkerRedisConfig      `json:"redis"`
	Queue      WorkerQueueConfig      `json:"queue"`
	SubQueues  []SubQueueConfig       `json:"sub_queues"`
	Deployment WorkerDeploymentConfig `json:"deployment"`
	Vaults     map[string]string      `json:"vaults,omitempty"`    // Decrypted vault key-value pairs
	GitToken   string                 `json:"git_token,omitempty"` // Resolved git token for authentication
}

// HandshakeErrorResponse is returned on handshake errors
type HandshakeErrorResponse struct {
	Error string `json:"error"`
}

// resolveGitToken resolves a git_token vault reference.
// Format: "vault://vault-name/key"
// Returns the decrypted value from the vault entry.
func resolveGitToken(ctx context.Context, tokenRef string, vaultStore *vaults.Store) (string, error) {
	if tokenRef == "" {
		return "", nil
	}

	if !strings.HasPrefix(tokenRef, "vault://") {
		return "", fmt.Errorf("git_token must use vault:// reference, got: %s", tokenRef)
	}

	// Parse vault://vault-name/key
	ref := strings.TrimPrefix(tokenRef, "vault://")
	parts := strings.SplitN(ref, "/", 2)
	if len(parts) != 2 {
		return "", fmt.Errorf("invalid vault reference format: %s (expected vault://vault-name/key)", tokenRef)
	}

	vaultName, key := parts[0], parts[1]

	value, _, err := vaultStore.GetEntry(ctx, vaultName, key)
	if err != nil {
		return "", fmt.Errorf("failed to get vault entry %s/%s: %w", vaultName, key, err)
	}

	return value, nil
}

// QueuesListResponse is the response for listing all queues
type QueuesListResponse struct {
	Queues []queueworker.QueueSummary `json:"queues"`
	Count  int                        `json:"count"`
}

// ReloadResponse is the response for reloading configurations
type ReloadResponse struct {
	Reloaded  []string `json:"reloaded"`
	Errors    []string `json:"errors"`
	Timestamp int64    `json:"timestamp"`
}

// CreateQueueRequest is the request body for creating a queue
type CreateQueueRequest struct {
	Name       string                      `json:"name" binding:"required"`
	Priority   int                         `json:"priority" binding:"required,min=1"`
	Provider   string                      `json:"provider,omitempty"`
	Deployment *queueworker.DeploymentConfig `json:"deployment,omitempty"`
}

// CreateQueueResponse is the response for queue creation
type CreateQueueResponse struct {
	Queue   *queueworker.QueueConfig `json:"queue"`
	Message string                   `json:"message"`
}

// WorkerHandshake handles worker registration and config retrieval
// Workers are trusted - they know their queue name and get the config directly
// @Summary Worker handshake
// @Description Register worker and retrieve queue configuration
// @Tags workers
// @Accept json
// @Produce json
// @Param request body HandshakeRequest true "Handshake request"
// @Success 200 {object} WorkerConfigResponse
// @Failure 400 {object} HandshakeErrorResponse
// @Failure 404 {object} HandshakeErrorResponse
// @Router /workers/handshake [post]
// @Router /worker/register [post]
func WorkerHandshake(store *queueworker.Store, cfg *config.Config) gin.HandlerFunc {
	return func(c *gin.Context) {
		var req HandshakeRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, HandshakeErrorResponse{
				Error: "invalid request: " + err.Error(),
			})
			return
		}

		if req.Queue == "" {
			c.JSON(http.StatusBadRequest, HandshakeErrorResponse{
				Error: "queue name is required",
			})
			return
		}

		queueName := req.Queue

		// Get all sub-queues for this queue (matching "queueName.*" pattern)
		subQueueConfigs, err := store.ListByPrefix(c.Request.Context(), queueName+queueworker.SubQueueSeparator)
		if err != nil {
			c.JSON(http.StatusInternalServerError, HandshakeErrorResponse{
				Error: "failed to retrieve sub-queue configurations",
			})
			return
		}

		// Build sub-queues list
		var subQueues []SubQueueConfig
		var queueCfg *queueworker.QueueConfig

		if len(subQueueConfigs) > 0 {
			// Use explicitly defined sub-queues
			for _, sq := range subQueueConfigs {
				subQueues = append(subQueues, SubQueueConfig{
					Name:     sq.Name,
					Priority: sq.Priority,
				})
			}
			// Use the first sub-queue config for deployment info
			queueCfg = subQueueConfigs[0]
		} else {
			// No sub-queues found with prefix pattern
			// If queue name doesn't have a sub-queue, try with .default suffix
			normalizedName := queueworker.NormalizeQueueName(queueName)

			queueCfg, err = store.Get(c.Request.Context(), normalizedName)
			if err != nil {
				c.JSON(http.StatusInternalServerError, HandshakeErrorResponse{
					Error: "failed to retrieve configuration",
				})
				return
			}

			if queueCfg == nil {
				// If normalized name was different, show helpful error
				if normalizedName != queueName {
					c.JSON(http.StatusNotFound, HandshakeErrorResponse{
						Error: fmt.Sprintf("queue '%s' not found (also tried '%s')", queueName, normalizedName),
					})
				} else {
					c.JSON(http.StatusNotFound, HandshakeErrorResponse{
						Error: fmt.Sprintf("queue '%s' not found", queueName),
					})
				}
				return
			}

			// Queue found - use its name directly as the sub-queue
			subQueues = []SubQueueConfig{{
				Name:     queueCfg.Name,
				Priority: queueCfg.Priority,
			}}
		}

		// Build deployment config
		var deployment WorkerDeploymentConfig
		var gitTokenRef string
		if queueCfg.Deployment != nil {
			redisStorage := false
			if queueCfg.Deployment.RedisStorage != nil {
				redisStorage = *queueCfg.Deployment.RedisStorage
			}
			deployment = WorkerDeploymentConfig{
				GitURL:             queueCfg.Deployment.GitURL,
				Branch:             queueCfg.Deployment.Branch,
				CodePath:           queueCfg.Deployment.CodePath,
				StartupCmd:         queueCfg.Deployment.StartupCmd,
				Mode:               queueCfg.Deployment.Mode,
				StartupTimeoutSecs: queueCfg.Deployment.StartupTimeoutSecs,
				RedisStorage:       redisStorage,
				Vaults:             queueCfg.Deployment.Vaults,
			}
			gitTokenRef = queueCfg.Deployment.GitToken
		}

		// Resolve vaults if configured
		var vaultData map[string]string
		if len(deployment.Vaults) > 0 {
			vaultStore := GetVaultStore()
			if vaultStore != nil && vaultStore.IsEnabled() {
				data, err := vaultStore.GetMultipleVaultsData(c.Request.Context(), deployment.Vaults)
				if err != nil {
					log.Printf("[VAULTS] Warning: failed to resolve vaults for queue %s: %v", queueName, err)
				} else {
					vaultData = data
					log.Printf("[VAULTS] Resolved %d vault entries for queue %s from vaults: %v", len(vaultData), queueName, deployment.Vaults)
				}
			} else {
				log.Printf("[VAULTS] Warning: vaults requested for queue %s but vaults feature is disabled", queueName)
			}
		}

		// Resolve git token if configured
		var gitToken string
		if gitTokenRef != "" {
			vaultStore := GetVaultStore()
			if vaultStore != nil && vaultStore.IsEnabled() {
				token, err := resolveGitToken(c.Request.Context(), gitTokenRef, vaultStore)
				if err != nil {
					log.Printf("[VAULTS] Warning: failed to resolve git_token for queue %s: %v", queueName, err)
				} else {
					gitToken = token
					log.Printf("[VAULTS] Resolved git_token for queue %s", queueName)
				}
			} else {
				log.Printf("[VAULTS] Warning: git_token requested for queue %s but vaults feature is disabled", queueName)
			}
		}

		// Return the full worker config response
		c.JSON(http.StatusOK, WorkerConfigResponse{
			Redis: WorkerRedisConfig{
				Addr:     fmt.Sprintf("%s:%s", cfg.RedisHost, cfg.RedisPort),
				Password: cfg.RedisPassword,
				TLS:      cfg.RedisTLS,
			},
			Queue: WorkerQueueConfig{
				Name:     queueName,
				Priority: queueCfg.Priority,
			},
			SubQueues:  subQueues,
			Deployment: deployment,
			Vaults:     vaultData,
			GitToken:   gitToken,
		})
	}
}

// GetQueueConfig returns configuration for a specific queue
// @Summary Get queue configuration
// @Description Retrieve queue configuration
// @Tags workers
// @Produce json
// @Param queue_name path string true "Queue name"
// @Success 200 {object} queueworker.QueueConfig
// @Failure 404 {object} map[string]string
// @Router /workers/config/{queue_name} [get]
func GetQueueConfig(store *queueworker.Store) gin.HandlerFunc {
	return func(c *gin.Context) {
		queueName := c.Param("queue_name")

		cfg, err := store.Get(c.Request.Context(), queueName)
		if err != nil || cfg == nil {
			c.JSON(http.StatusNotFound, gin.H{"error": "queue not found"})
			return
		}

		c.JSON(http.StatusOK, cfg)
	}
}

// ListQueueConfigs returns all registered queues
// @Summary List all queue configurations
// @Description List all registered queue configurations
// @Tags workers
// @Produce json
// @Success 200 {object} QueuesListResponse
// @Failure 500 {object} map[string]string
// @Router /workers/queues [get]
func ListQueueConfigs(store *queueworker.Store) gin.HandlerFunc {
	return func(c *gin.Context) {
		queues, err := store.ListQueues(c.Request.Context())
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}

		// Get summary for each queue
		summaries := make([]queueworker.QueueSummary, 0, len(queues))
		for _, q := range queues {
			cfg, err := store.Get(c.Request.Context(), q)
			if err != nil || cfg == nil {
				continue
			}
			summaries = append(summaries, queueworker.QueueSummary{
				Name:     cfg.Name,
				Priority: cfg.Priority,
				Provider: cfg.Provider,
			})
		}

		c.JSON(http.StatusOK, QueuesListResponse{
			Queues: summaries,
			Count:  len(summaries),
		})
	}
}

// CreateQueueConfig creates a new queue configuration
// @Summary Create a new queue configuration
// @Description Create a new queue configuration from JSON payload
// @Tags workers
// @Accept json
// @Produce json
// @Param request body CreateQueueRequest true "Queue configuration"
// @Param force query bool false "Force update if queue already exists"
// @Success 201 {object} CreateQueueResponse
// @Failure 400 {object} map[string]string
// @Failure 409 {object} map[string]string
// @Failure 500 {object} map[string]string
// @Router /workers/queues [post]
func CreateQueueConfig(store *queueworker.Store) gin.HandlerFunc {
	return func(c *gin.Context) {
		var req CreateQueueRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}

		// Check for force query parameter
		force := c.Query("force") == "true"

		// Validate deployment if provided
		if req.Deployment != nil {
			if req.Deployment.GitURL == "" {
				c.JSON(http.StatusBadRequest, gin.H{"error": "git_url is required for deployment"})
				return
			}
			if req.Deployment.StartupCmd == "" {
				c.JSON(http.StatusBadRequest, gin.H{"error": "startup_cmd is required for deployment"})
				return
			}
		}

		ctx := c.Request.Context()

		// Check if queue already exists
		exists, err := store.Exists(ctx, req.Name)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to check queue existence: " + err.Error()})
			return
		}

		if exists && !force {
			c.JSON(http.StatusConflict, gin.H{
				"error": fmt.Sprintf("queue '%s' already exists. Use --force to update existing queue", req.Name),
			})
			return
		}

		now := time.Now().Unix()
		cfg := &queueworker.QueueConfig{
			Name:       req.Name,
			Priority:   req.Priority,
			Provider:   req.Provider,
			Deployment: req.Deployment,
			CreatedAt:  now,
			UpdatedAt:  now,
		}

		if err := store.Save(ctx, cfg); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}

		// Register in asynq so it appears in monitoring
		if err := store.RegisterAsynqQueues(ctx, []string{req.Name}); err != nil {
			log.Printf("Warning: failed to register queue in asynq: %v", err)
		}

		message := "Queue created successfully"
		if exists {
			message = "Queue updated successfully"
		}

		c.JSON(http.StatusCreated, CreateQueueResponse{
			Queue:   cfg,
			Message: message,
		})
	}
}

// DeleteQueueConfig deletes a queue configuration
// @Summary Delete a queue configuration
// @Description Delete a queue configuration by name. By default fails if queue has pending tasks. Use force=true to delete anyway.
// @Tags workers
// @Produce json
// @Param queue_name path string true "Queue name"
// @Param force query bool false "Force deletion even if queue has pending tasks"
// @Success 200 {object} map[string]string
// @Failure 400 {object} map[string]string "Queue has pending tasks"
// @Failure 404 {object} map[string]string
// @Failure 500 {object} map[string]string
// @Router /workers/queues/{queue_name} [delete]
func DeleteQueueConfig(store *queueworker.Store) gin.HandlerFunc {
	return func(c *gin.Context) {
		queueName := c.Param("queue_name")
		if queueName == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "queue name is required"})
			return
		}

		force := c.Query("force") == "true"
		ctx := c.Request.Context()

		// Check if queue exists
		exists, err := store.Exists(ctx, queueName)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to check queue existence: " + err.Error()})
			return
		}

		if !exists {
			c.JSON(http.StatusNotFound, gin.H{"error": fmt.Sprintf("queue '%s' not found", queueName)})
			return
		}

		// Check if queue has tasks (unless force)
		if !force {
			stats, err := store.GetQueueStats(ctx, queueName)
			if err == nil && stats != nil && !stats.IsEmpty {
				c.JSON(http.StatusBadRequest, gin.H{
					"error": fmt.Sprintf("queue '%s' has pending tasks", queueName),
					"stats": stats,
					"hint":  "Use ?force=true to delete anyway",
				})
				return
			}
		}

		// Delete the queue with full Redis cleanup
		if err := store.DeleteQueueFull(ctx, queueName, force); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}

		c.JSON(http.StatusOK, gin.H{
			"message": fmt.Sprintf("Queue '%s' deleted successfully (including all Redis data)", queueName),
		})
	}
}

// ReloadQueueConfigs reloads YAML configurations (admin only - uses global API key)
// @Summary Reload queue configurations from YAML files
// @Description Reload all queue configurations from YAML files (admin only)
// @Tags workers
// @Produce json
// @Success 200 {object} ReloadResponse
// @Router /workers/reload [post]
func ReloadQueueConfigs(store *queueworker.Store, configDir string) gin.HandlerFunc {
	return func(c *gin.Context) {
		reloaded, errors := reloadFromYAML(c.Request.Context(), store, configDir)
		c.JSON(http.StatusOK, ReloadResponse{
			Reloaded:  reloaded,
			Errors:    errors,
			Timestamp: time.Now().Unix(),
		})
	}
}

// ReloadFromYAMLContext is an exported version for use by file watcher
func ReloadFromYAMLContext(ctx context.Context, store *queueworker.Store, dir string) ([]string, []string) {
	return reloadFromYAML(ctx, store, dir)
}

// reloadFromYAML loads configurations from YAML files and saves to DB
func reloadFromYAML(ctx context.Context, store *queueworker.Store, dir string) ([]string, []string) {
	var reloaded []string
	var errors []string

	yamls, err := queueworker.LoadAll(dir)
	if err != nil {
		errors = append(errors, "failed to load YAML files: "+err.Error())
		return reloaded, errors
	}

	for _, y := range yamls {
		for queueName, queueCfg := range y.Queues {
			// Convert to runtime configs (handles sub_queues)
			configs := queueCfg.ToQueueConfigs(queueName)

			for _, cfg := range configs {
				// Save to DB
				if err := store.Save(ctx, cfg); err != nil {
					errors = append(errors, cfg.Name+": failed to save: "+err.Error())
					continue
				}

				reloaded = append(reloaded, cfg.Name)
				log.Printf("[QUEUEWORKER] Loaded: %s (provider=%s, priority=%d)", cfg.Name, cfg.Provider, cfg.Priority)
			}
		}
	}

	// Register queues in asynq so they appear in asynqmon
	if len(reloaded) > 0 {
		if err := store.RegisterAsynqQueues(ctx, reloaded); err != nil {
			log.Printf("[QUEUEWORKER] Warning: failed to register asynq queues: %v", err)
		} else {
			log.Printf("[QUEUEWORKER] Registered queues in asynq: %v", reloaded)
		}
	}

	return reloaded, errors
}

// LoadQueueWorkersAtStartup is called during application startup to load YAML configs
// Returns the list of provider types found in the configs for provider registration
func LoadQueueWorkersAtStartup(store *queueworker.Store, configDir string) ([]string, error) {
	ctx := context.Background()
	reloaded, providerTypes, errors := reloadFromYAMLWithProviders(ctx, store, configDir)

	if len(errors) > 0 {
		for _, e := range errors {
			log.Printf("[QUEUEWORKER] Warning: %s", e)
		}
	}

	if len(reloaded) > 0 {
		log.Printf("[QUEUEWORKER] Loaded %d queue configurations: %v", len(reloaded), reloaded)

		// Register queues in asynq so they appear in asynqmon
		if err := store.RegisterAsynqQueues(ctx, reloaded); err != nil {
			log.Printf("[QUEUEWORKER] Warning: failed to register asynq queues: %v", err)
		} else {
			log.Printf("[QUEUEWORKER] Registered queues in asynq: %v", reloaded)
		}
	} else {
		log.Printf("[QUEUEWORKER] No queue configurations loaded from %s", configDir)
	}

	return providerTypes, nil
}

// reloadFromYAMLWithProviders loads configs and returns provider types
func reloadFromYAMLWithProviders(ctx context.Context, store *queueworker.Store, dir string) ([]string, []string, []string) {
	var reloaded []string
	var providerTypes []string
	var errors []string

	yamls, err := queueworker.LoadAll(dir)
	if err != nil {
		errors = append(errors, "failed to load YAML files: "+err.Error())
		return reloaded, providerTypes, errors
	}

	for _, y := range yamls {
		for queueName, queueCfg := range y.Queues {
			// Collect provider type
			if queueCfg.Provider != "" && queueCfg.Provider != "worker" {
				providerTypes = append(providerTypes, queueCfg.Provider)
			}

			// Convert to runtime configs (handles sub_queues)
			configs := queueCfg.ToQueueConfigs(queueName)

			for _, cfg := range configs {
				// Save to DB
				if err := store.Save(ctx, cfg); err != nil {
					errors = append(errors, cfg.Name+": failed to save: "+err.Error())
					continue
				}

				reloaded = append(reloaded, cfg.Name)
				log.Printf("[QUEUEWORKER] Loaded: %s (provider=%s, priority=%d)", cfg.Name, cfg.Provider, cfg.Priority)
			}
		}
	}

	return reloaded, providerTypes, errors
}
