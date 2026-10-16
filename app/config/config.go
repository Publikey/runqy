package config

import (
	"fmt"
	"net/url"
	"os"
	"strconv"
	"strings"
)

type Config struct {
	// Server
	HTTPPort string

	// API Authentication
	APIKey       string // RUNQY_API_KEY — admin key (full access, backward compatible)
	WorkerAPIKey string // RUNQY_WORKER_API_KEY — worker role (register, dequeue, heartbeat)
	ClientAPIKey string // RUNQY_CLIENT_API_KEY — client role (enqueue, task status)

	// Redis (existing env vars, centralized here)
	RedisHost     string
	RedisPort     string
	RedisPassword string
	RedisTLS      bool

	// Queue Workers
	QueueWorkersDir string

	// PostgreSQL
	PostgresHost     string
	PostgresPort     string
	PostgresUser     string
	PostgresPassword string
	PostgresDB       string
	PostgresSSL      string

	// SQLite (alternative to PostgreSQL for local development)
	UseSQLite    bool
	SQLiteDBPath string

	// Monitoring
	ReadOnly          bool
	PrometheusAddress string

	// Security
	JWTSecret      string
	VaultMasterKey string

	// Request limits
	MaxBodySize int64 // Maximum request body size in bytes (0 = default 50MB)

	// GitHub Repository Config
	ConfigRepoURL       string
	ConfigRepoBranch    string
	ConfigRepoPath      string
	ConfigCloneDir      string
	GitHubPAT           string // Personal Access Token for HTTPS auth
	ConfigWatchInterval int
}

// Load creates a Config from environment variables
func Load() *Config {
	return &Config{
		// Server
		HTTPPort: getEnv("PORT", "3000"),

		// API Authentication
		APIKey:       os.Getenv("RUNQY_API_KEY"),
		WorkerAPIKey: os.Getenv("RUNQY_WORKER_API_KEY"),
		ClientAPIKey: os.Getenv("RUNQY_CLIENT_API_KEY"),

		// Redis
		RedisHost:     getEnv("REDIS_HOST", "localhost"),
		RedisPort:     getEnv("REDIS_PORT", "6379"),
		RedisPassword: os.Getenv("REDIS_PASSWORD"),
		RedisTLS:      os.Getenv("REDIS_TLS") == "true",

		// Queue Workers
		QueueWorkersDir: getEnv("QUEUE_WORKERS_DIR", "../queueworkers"),

		// PostgreSQL
		PostgresHost:     getEnv("DATABASE_HOST", "localhost"),
		PostgresPort:     getEnv("DATABASE_PORT", "5432"),
		PostgresUser:     getEnv("DATABASE_USER", "postgres"),
		PostgresPassword: os.Getenv("DATABASE_PASSWORD"),
		PostgresDB:       getEnv("DATABASE_DBNAME", "sdxl_queuing_dev"),
		PostgresSSL:      getEnv("DATABASE_SSL", "disable"),

		// SQLite: --sqlite flag, or RUNQY_USE_SQLITE=true where the command line is hard
		// to change (container platforms).
		UseSQLite:    strings.EqualFold(os.Getenv("RUNQY_USE_SQLITE"), "true"),
		SQLiteDBPath: getEnv("SQLITE_DB_PATH", "runqy.db"),

		// Monitoring
		ReadOnly:          os.Getenv("ASYNQ_READ_ONLY") == "true",
		PrometheusAddress: os.Getenv("PROMETHEUS_ADDRESS"),

		// Security
		JWTSecret:      os.Getenv("RUNQY_JWT_SECRET"),
		VaultMasterKey: os.Getenv("RUNQY_VAULT_MASTER_KEY"),

		// Request limits
		MaxBodySize: getEnvInt64("RUNQY_MAX_BODY_SIZE", 50*1024*1024), // default 50MB

		// GitHub Repository Config
		ConfigRepoURL:       os.Getenv("CONFIG_REPO_URL"),
		ConfigRepoBranch:    getEnv("CONFIG_REPO_BRANCH", "main"),
		ConfigRepoPath:      getEnv("CONFIG_REPO_PATH", "deployment"),
		ConfigCloneDir:      getEnv("CONFIG_CLONE_DIR", "downloads"),
		GitHubPAT:           os.Getenv("GITHUB_PAT"),
		ConfigWatchInterval: getEnvInt("CONFIG_WATCH_INTERVAL", 60),
	}
}

func getEnv(key, defaultVal string) string {
	if val := os.Getenv(key); val != "" {
		return val
	}
	return defaultVal
}

func getEnvInt(key string, defaultVal int) int {
	if val := os.Getenv(key); val != "" {
		if i, err := strconv.Atoi(val); err == nil {
			return i
		}
	}
	return defaultVal
}

func getEnvInt64(key string, defaultVal int64) int64 {
	if val := os.Getenv(key); val != "" {
		if i, err := strconv.ParseInt(val, 10, 64); err == nil {
			return i
		}
	}
	return defaultVal
}

// ParseRedisURI parses a Redis URI (redis[s]://[:password@]host[:port]) and
// sets RedisHost, RedisPort, RedisPassword, and RedisTLS on the Config.
func (c *Config) ParseRedisURI(uri string) error {
	// Normalize scheme for url.Parse
	useTLS := false
	if strings.HasPrefix(uri, "rediss://") {
		useTLS = true
		uri = "redis://" + strings.TrimPrefix(uri, "rediss://")
	}

	u, err := url.Parse(uri)
	if err != nil {
		return fmt.Errorf("invalid redis URI: %w", err)
	}

	if u.Scheme != "redis" {
		return fmt.Errorf("invalid redis URI scheme: %s (expected redis:// or rediss://)", u.Scheme)
	}

	host := u.Hostname()
	if host != "" {
		c.RedisHost = host
	}

	port := u.Port()
	if port != "" {
		c.RedisPort = port
	}

	if u.User != nil {
		if pw, ok := u.User.Password(); ok {
			c.RedisPassword = pw
		}
	}

	c.RedisTLS = useTLS
	return nil
}
