package monitoring

import (
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/Publikey/runqy/config"
	queueworker "github.com/Publikey/runqy/queues"
)

// EnvVar is a single environment variable (name + display value) for the System page.
type EnvVar struct {
	Name  string `json:"name"`
	Value string `json:"value"`
}

// SystemEnvInfo is the response for GET /api/system/env: an ordered, read-only view of the
// server's runqy configuration variables. Secret values are masked, never returned in clear.
type SystemEnvInfo struct {
	Vars []EnvVar `json:"vars"`
}

// maskSecret hides a secret's value while still showing whether it is configured.
func maskSecret(raw string) string {
	if raw == "" {
		return "(not set)"
	}
	return "•••••• (set)"
}

// newSystemEnvHandlerFunc returns a handler that exposes the server's runqy environment
// variables (effective values, defaults included) for at-a-glance inspection in the dashboard.
// It returns a curated list — not os.Environ() — and masks secrets. The endpoint is protected
// by the dashboard auth middleware like the other /api/* routes.
func newSystemEnvHandlerFunc(cfg *config.Config) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		resp := SystemEnvInfo{Vars: []EnvVar{}}
		add := func(name, value string) {
			resp.Vars = append(resp.Vars, EnvVar{Name: name, Value: value})
		}

		if cfg != nil {
			// Server
			add("PORT", cfg.HTTPPort)

			// API authentication (secrets)
			add("RUNQY_API_KEY", maskSecret(cfg.APIKey))
			add("RUNQY_WORKER_API_KEY", maskSecret(cfg.WorkerAPIKey))
			add("RUNQY_CLIENT_API_KEY", maskSecret(cfg.ClientAPIKey))

			// Redis
			add("REDIS_HOST", cfg.RedisHost)
			add("REDIS_PORT", cfg.RedisPort)
			add("REDIS_TLS", strconv.FormatBool(cfg.RedisTLS))
			add("REDIS_PASSWORD", maskSecret(cfg.RedisPassword))

			// PostgreSQL
			add("DATABASE_HOST", cfg.PostgresHost)
			add("DATABASE_PORT", cfg.PostgresPort)
			add("DATABASE_USER", cfg.PostgresUser)
			add("DATABASE_DBNAME", cfg.PostgresDB)
			add("DATABASE_SSL", cfg.PostgresSSL)
			add("DATABASE_PASSWORD", maskSecret(cfg.PostgresPassword))

			// SQLite
			add("SQLITE_DB_PATH", cfg.SQLiteDBPath)

			// Queue workers / config repo
			add("QUEUE_WORKERS_DIR", cfg.QueueWorkersDir)
			add("CONFIG_REPO_URL", cfg.ConfigRepoURL)
			add("CONFIG_REPO_BRANCH", cfg.ConfigRepoBranch)
			add("CONFIG_REPO_PATH", cfg.ConfigRepoPath)
			add("CONFIG_CLONE_DIR", cfg.ConfigCloneDir)
			add("CONFIG_WATCH_INTERVAL", strconv.Itoa(cfg.ConfigWatchInterval))
			add("GITHUB_PAT", maskSecret(cfg.GitHubPAT))

			// Monitoring
			add("ASYNQ_READ_ONLY", strconv.FormatBool(cfg.ReadOnly))
			add("PROMETHEUS_ADDRESS", cfg.PrometheusAddress)

			// Security (secrets)
			add("RUNQY_JWT_SECRET", maskSecret(cfg.JWTSecret))
			add("RUNQY_VAULT_MASTER_KEY", maskSecret(cfg.VaultMasterKey))

			// Request limits
			add("RUNQY_MAX_BODY_SIZE", strconv.FormatInt(cfg.MaxBodySize, 10))
		}

		// Task lifecycle defaults (effective values, defaults included)
		limits := queueworker.DefaultLimits()
		add("RUNQY_DEFAULT_MAX_RETRY", strconv.Itoa(limits.MaxRetry))
		add("RUNQY_DEFAULT_TTL_COMPLETED", limits.TTLCompleted.String())
		add("RUNQY_DEFAULT_TTL_ARCHIVED", limits.TTLArchived.String())
		add("RUNQY_DEFAULT_PENDING_TIMEOUT", limits.PendingTimeout.String())
		add("RUNQY_DEFAULT_ACTIVE_TIMEOUT", limits.ActiveTimeout.String())

		if err := json.NewEncoder(w).Encode(resp); err != nil {
			writeJSONError(w, err.Error(), http.StatusInternalServerError)
			return
		}
	}
}
