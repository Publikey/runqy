package monitoring

import (
	"embed"
	"fmt"
	"net/http"
	"strings"

	"github.com/Publikey/runqy/auth"
	"github.com/Publikey/runqy/config"
	queueworker "github.com/Publikey/runqy/queues"
	"github.com/Publikey/runqy/vaults"
	"github.com/gorilla/mux"
	"github.com/Publikey/runqy/third_party/asynq"
	"github.com/jmoiron/sqlx"
	"github.com/redis/go-redis/v9"
)

// Options are used to configure HTTPHandler.
type Options struct {
	// URL path the handler is responsible for.
	// The path is used for the homepage of asynqmon, and every other page is rooted in this subtree.
	//
	// This field is optional. Default is "/".
	RootPath string

	// RedisConnOpt specifies the connection to a redis-server or redis-cluster.
	//
	// This field is required.
	RedisConnOpt asynq.RedisConnOpt

	// PayloadFormatter is used to convert payload bytes to string shown in the UI.
	//
	// This field is optional.
	PayloadFormatter PayloadFormatter

	// ResultFormatter is used to convert result bytes to string shown in the UI.
	//
	// This field is optional.
	ResultFormatter ResultFormatter

	// PrometheusAddress specifies the address of the Prometheus to connect to.
	//
	// This field is optional. If this field is set, asynqmon will query the Prometheus server
	// to get the time series data about queue metrics and show them in the web UI.
	PrometheusAddress string

	// Set ReadOnly to true to restrict user to view-only mode.
	ReadOnly bool

	// DB is the database connection for retrieving database info.
	//
	// This field is optional.
	DB *sqlx.DB

	// VaultStore is the vault store for accessing encrypted vaults.
	//
	// This field is optional.
	VaultStore *vaults.Store

	// QueueStore is the queue worker store for managing queue configurations.
	//
	// This field is optional.
	QueueStore *queueworker.Store

	// AuthStore is the authentication store for admin user management.
	//
	// This field is optional. If nil, authentication is disabled.
	AuthStore *auth.Store

	// Config is the centralized server configuration.
	//
	// This field is optional. Used by database info handler to avoid os.Getenv.
	Config *config.Config
}

// HTTPHandler is a http.Handler for asynqmon application.
type HTTPHandler struct {
	router   *mux.Router
	closers  []func() error
	rootPath string // the value should not have the trailing slash
}

func (h *HTTPHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	h.router.ServeHTTP(w, r)
}

// New creates a HTTPHandler with the given options.
// Returns an error instead of panicking if configuration is invalid.
func New(opts Options) (*HTTPHandler, error) {
	if opts.RedisConnOpt == nil {
		return nil, fmt.Errorf("asynqmon.New: RedisConnOpt field is required")
	}
	rc, ok := opts.RedisConnOpt.MakeRedisClient().(redis.UniversalClient)
	if !ok {
		return nil, fmt.Errorf("asynqmon.New: unsupported RedisConnOpt type %T", opts.RedisConnOpt)
	}
	i := asynq.NewInspector(opts.RedisConnOpt)

	// Make sure that RootPath starts with a slash if provided.
	if opts.RootPath != "" && !strings.HasPrefix(opts.RootPath, "/") {
		rc.Close()
		i.Close()
		return nil, fmt.Errorf("asynqmon.New: RootPath must start with a slash")
	}
	// Remove tailing slash from RootPath.
	opts.RootPath = strings.TrimSuffix(opts.RootPath, "/")

	return &HTTPHandler{
		router:   muxRouter(opts, rc, i, opts.DB),
		closers:  []func() error{rc.Close, i.Close},
		rootPath: opts.RootPath,
	}, nil
}

// Close closes connections to redis.
func (h *HTTPHandler) Close() error {
	for _, f := range h.closers {
		if err := f(); err != nil {
			return err
		}
	}
	return nil
}

// RootPath returns the root URL path used for asynqmon application.
// Returned path string does not have the trailing slash.
func (h *HTTPHandler) RootPath() string {
	return h.rootPath
}

//go:embed all:ui/build
var staticContents embed.FS

func muxRouter(opts Options, rc redis.UniversalClient, inspector *asynq.Inspector, db *sqlx.DB) *mux.Router {
	router := mux.NewRouter().PathPrefix(opts.RootPath).Subrouter()

	var payloadFmt PayloadFormatter = DefaultPayloadFormatter
	if opts.PayloadFormatter != nil {
		payloadFmt = opts.PayloadFormatter
	}

	var resultFmt ResultFormatter = DefaultResultFormatter
	if opts.ResultFormatter != nil {
		resultFmt = opts.ResultFormatter
	}

	api := router.PathPrefix("/api").Subrouter()

	// Auth endpoints (public, no middleware)
	if opts.AuthStore != nil {
		api.HandleFunc("/auth/status", newAuthStatusHandlerFunc(opts.AuthStore)).Methods("GET")
		api.HandleFunc("/auth/setup", newAuthSetupHandlerFunc(opts.AuthStore)).Methods("POST")
		api.HandleFunc("/auth/login", newAuthLoginHandlerFunc(opts.AuthStore)).Methods("POST")
		api.HandleFunc("/auth/logout", newAuthLogoutHandlerFunc()).Methods("POST")
	}

	// Queue endpoints.
	api.HandleFunc("/queues", newListQueuesHandlerFunc(inspector)).Methods("GET")
	api.HandleFunc("/queues/{qname}", newGetQueueHandlerFunc(inspector)).Methods("GET")
	api.HandleFunc("/queues/{qname}", newDeleteQueueHandlerFunc(inspector, opts.QueueStore)).Methods("DELETE")
	api.HandleFunc("/queues/{qname}:pause", newPauseQueueHandlerFunc(inspector)).Methods("POST")
	api.HandleFunc("/queues/{qname}:resume", newResumeQueueHandlerFunc(inspector)).Methods("POST")
	api.HandleFunc("/queues/{qname}:restore", newRestoreQueueHandlerFunc(opts.QueueStore)).Methods("POST")

	// Queue Historical Stats endpoint.
	api.HandleFunc("/queue_stats", newListQueueStatsHandlerFunc(inspector)).Methods("GET")

	// Task endpoints.
	api.HandleFunc("/queues/{qname}/active_tasks", newListActiveTasksHandlerFunc(inspector, payloadFmt)).Methods("GET")
	api.HandleFunc("/queues/{qname}/active_tasks/{task_id}:cancel", newCancelActiveTaskHandlerFunc(inspector)).Methods("POST")
	api.HandleFunc("/queues/{qname}/active_tasks:cancel_all", newCancelAllActiveTasksHandlerFunc(inspector)).Methods("POST")
	api.HandleFunc("/queues/{qname}/active_tasks:batch_cancel", newBatchCancelActiveTasksHandlerFunc(inspector)).Methods("POST")

	api.HandleFunc("/queues/{qname}/pending_tasks", newListPendingTasksHandlerFunc(inspector, payloadFmt)).Methods("GET")
	api.HandleFunc("/queues/{qname}/pending_tasks/{task_id}", newDeleteTaskHandlerFunc(inspector)).Methods("DELETE")
	api.HandleFunc("/queues/{qname}/pending_tasks:delete_all", newDeleteAllPendingTasksHandlerFunc(inspector)).Methods("DELETE")
	api.HandleFunc("/queues/{qname}/pending_tasks:batch_delete", newBatchDeleteTasksHandlerFunc(inspector)).Methods("POST")
	api.HandleFunc("/queues/{qname}/pending_tasks/{task_id}:archive", newArchiveTaskHandlerFunc(inspector)).Methods("POST")
	api.HandleFunc("/queues/{qname}/pending_tasks:archive_all", newArchiveAllPendingTasksHandlerFunc(inspector)).Methods("POST")
	api.HandleFunc("/queues/{qname}/pending_tasks:batch_archive", newBatchArchiveTasksHandlerFunc(inspector)).Methods("POST")

	api.HandleFunc("/queues/{qname}/scheduled_tasks", newListScheduledTasksHandlerFunc(inspector, payloadFmt)).Methods("GET")
	api.HandleFunc("/queues/{qname}/scheduled_tasks/{task_id}", newDeleteTaskHandlerFunc(inspector)).Methods("DELETE")
	api.HandleFunc("/queues/{qname}/scheduled_tasks:delete_all", newDeleteAllScheduledTasksHandlerFunc(inspector)).Methods("DELETE")
	api.HandleFunc("/queues/{qname}/scheduled_tasks:batch_delete", newBatchDeleteTasksHandlerFunc(inspector)).Methods("POST")
	api.HandleFunc("/queues/{qname}/scheduled_tasks/{task_id}:run", newRunTaskHandlerFunc(inspector)).Methods("POST")
	api.HandleFunc("/queues/{qname}/scheduled_tasks:run_all", newRunAllScheduledTasksHandlerFunc(inspector)).Methods("POST")
	api.HandleFunc("/queues/{qname}/scheduled_tasks:batch_run", newBatchRunTasksHandlerFunc(inspector)).Methods("POST")
	api.HandleFunc("/queues/{qname}/scheduled_tasks/{task_id}:archive", newArchiveTaskHandlerFunc(inspector)).Methods("POST")
	api.HandleFunc("/queues/{qname}/scheduled_tasks:archive_all", newArchiveAllScheduledTasksHandlerFunc(inspector)).Methods("POST")
	api.HandleFunc("/queues/{qname}/scheduled_tasks:batch_archive", newBatchArchiveTasksHandlerFunc(inspector)).Methods("POST")

	api.HandleFunc("/queues/{qname}/retry_tasks", newListRetryTasksHandlerFunc(inspector, payloadFmt)).Methods("GET")
	api.HandleFunc("/queues/{qname}/retry_tasks/{task_id}", newDeleteTaskHandlerFunc(inspector)).Methods("DELETE")
	api.HandleFunc("/queues/{qname}/retry_tasks:delete_all", newDeleteAllRetryTasksHandlerFunc(inspector)).Methods("DELETE")
	api.HandleFunc("/queues/{qname}/retry_tasks:batch_delete", newBatchDeleteTasksHandlerFunc(inspector)).Methods("POST")
	api.HandleFunc("/queues/{qname}/retry_tasks/{task_id}:run", newRunTaskHandlerFunc(inspector)).Methods("POST")
	api.HandleFunc("/queues/{qname}/retry_tasks:run_all", newRunAllRetryTasksHandlerFunc(inspector)).Methods("POST")
	api.HandleFunc("/queues/{qname}/retry_tasks:batch_run", newBatchRunTasksHandlerFunc(inspector)).Methods("POST")
	api.HandleFunc("/queues/{qname}/retry_tasks/{task_id}:archive", newArchiveTaskHandlerFunc(inspector)).Methods("POST")
	api.HandleFunc("/queues/{qname}/retry_tasks:archive_all", newArchiveAllRetryTasksHandlerFunc(inspector)).Methods("POST")
	api.HandleFunc("/queues/{qname}/retry_tasks:batch_archive", newBatchArchiveTasksHandlerFunc(inspector)).Methods("POST")

	api.HandleFunc("/queues/{qname}/archived_tasks", newListArchivedTasksHandlerFunc(inspector, payloadFmt)).Methods("GET")
	api.HandleFunc("/queues/{qname}/archived_tasks/{task_id}", newDeleteTaskHandlerFunc(inspector)).Methods("DELETE")
	api.HandleFunc("/queues/{qname}/archived_tasks:delete_all", newDeleteAllArchivedTasksHandlerFunc(inspector)).Methods("DELETE")
	api.HandleFunc("/queues/{qname}/archived_tasks:batch_delete", newBatchDeleteTasksHandlerFunc(inspector)).Methods("POST")
	api.HandleFunc("/queues/{qname}/archived_tasks/{task_id}:run", newRunTaskHandlerFunc(inspector)).Methods("POST")
	api.HandleFunc("/queues/{qname}/archived_tasks:run_all", newRunAllArchivedTasksHandlerFunc(inspector)).Methods("POST")
	api.HandleFunc("/queues/{qname}/archived_tasks:batch_run", newBatchRunTasksHandlerFunc(inspector)).Methods("POST")

	api.HandleFunc("/queues/{qname}/completed_tasks", newListCompletedTasksHandlerFunc(inspector, payloadFmt, resultFmt)).Methods("GET")
	api.HandleFunc("/queues/{qname}/completed_tasks/{task_id}", newDeleteTaskHandlerFunc(inspector)).Methods("DELETE")
	api.HandleFunc("/queues/{qname}/completed_tasks:delete_all", newDeleteAllCompletedTasksHandlerFunc(inspector)).Methods("DELETE")
	api.HandleFunc("/queues/{qname}/completed_tasks:batch_delete", newBatchDeleteTasksHandlerFunc(inspector)).Methods("POST")

	api.HandleFunc("/queues/{qname}/groups/{gname}/aggregating_tasks", newListAggregatingTasksHandlerFunc(inspector, payloadFmt)).Methods("GET")
	api.HandleFunc("/queues/{qname}/groups/{gname}/aggregating_tasks/{task_id}", newDeleteTaskHandlerFunc(inspector)).Methods("DELETE")
	api.HandleFunc("/queues/{qname}/groups/{gname}/aggregating_tasks:delete_all", newDeleteAllAggregatingTasksHandlerFunc(inspector)).Methods("DELETE")
	api.HandleFunc("/queues/{qname}/groups/{gname}/aggregating_tasks:batch_delete", newBatchDeleteTasksHandlerFunc(inspector)).Methods("POST")
	api.HandleFunc("/queues/{qname}/groups/{gname}/aggregating_tasks/{task_id}:run", newRunTaskHandlerFunc(inspector)).Methods("POST")
	api.HandleFunc("/queues/{qname}/groups/{gname}/aggregating_tasks:run_all", newRunAllAggregatingTasksHandlerFunc(inspector)).Methods("POST")
	api.HandleFunc("/queues/{qname}/groups/{gname}/aggregating_tasks:batch_run", newBatchRunTasksHandlerFunc(inspector)).Methods("POST")
	api.HandleFunc("/queues/{qname}/groups/{gname}/aggregating_tasks/{task_id}:archive", newArchiveTaskHandlerFunc(inspector)).Methods("POST")
	api.HandleFunc("/queues/{qname}/groups/{gname}/aggregating_tasks:archive_all", newArchiveAllAggregatingTasksHandlerFunc(inspector)).Methods("POST")
	api.HandleFunc("/queues/{qname}/groups/{gname}/aggregating_tasks:batch_archive", newBatchArchiveTasksHandlerFunc(inspector)).Methods("POST")

	api.HandleFunc("/queues/{qname}/tasks/{task_id}", newGetTaskHandlerFunc(inspector, payloadFmt, resultFmt)).Methods("GET")

	// Groups endponts
	api.HandleFunc("/queues/{qname}/groups", newListGroupsHandlerFunc(inspector)).Methods("GET")

	// Servers endpoints (asynq internal servers).
	api.HandleFunc("/servers", newListServersHandlerFunc(inspector, payloadFmt)).Methods("GET")

	// Workers endpoints (external workers from asynq:workers:* keys).
	api.HandleFunc("/workers", newListWorkersHandlerFunc(rc)).Methods("GET")
	api.HandleFunc("/workers/{workerId}/logs", newGetWorkerLogsHandlerFunc(rc)).Methods("GET")
	api.HandleFunc("/workers/{workerId}/logs/stream", newWorkerLogStreamHandlerFunc(rc)).Methods("GET")

	// Scheduler Entry endpoints.
	api.HandleFunc("/scheduler_entries", newListSchedulerEntriesHandlerFunc(inspector, payloadFmt)).Methods("GET")
	api.HandleFunc("/scheduler_entries/{entry_id}/enqueue_events", newListSchedulerEnqueueEventsHandlerFunc(inspector)).Methods("GET")

	// Redis info endpoint.
	switch c := rc.(type) {
	case *redis.ClusterClient:
		api.HandleFunc("/redis_info", newRedisClusterInfoHandlerFunc(c, inspector)).Methods("GET")
	case *redis.Client:
		api.HandleFunc("/redis_info", newRedisInfoHandlerFunc(c)).Methods("GET")
	}

	// Database info endpoint.
	if db != nil {
		api.HandleFunc("/database_info", newDatabaseInfoHandlerFunc(db, opts.Config)).Methods("GET")
	}

	// System environment variables (read-only, secrets masked).
	api.HandleFunc("/system/env", newSystemEnvHandlerFunc(opts.Config)).Methods("GET")

	// Vaults endpoints.
	if opts.VaultStore != nil {
		api.HandleFunc("/vaults", newListVaultsHandlerFunc(opts.VaultStore)).Methods("GET")
		api.HandleFunc("/vaults", newCreateVaultHandlerFunc(opts.VaultStore)).Methods("POST")
		api.HandleFunc("/vaults/{name}", newGetVaultHandlerFunc(opts.VaultStore)).Methods("GET")
		api.HandleFunc("/vaults/{name}", newDeleteVaultHandlerFunc(opts.VaultStore)).Methods("DELETE")
		api.HandleFunc("/vaults/{name}/entries", newSetEntryHandlerFunc(opts.VaultStore)).Methods("POST")
		api.HandleFunc("/vaults/{name}/entries", newListEntriesHandlerFunc(opts.VaultStore)).Methods("GET")
		api.HandleFunc("/vaults/{name}/entries/{key}", newDeleteEntryHandlerFunc(opts.VaultStore)).Methods("DELETE")
	}

	// Queue config endpoints.
	if opts.QueueStore != nil {
		api.HandleFunc("/queue_configs", newListQueueConfigsHandlerFunc(opts.QueueStore)).Methods("GET")
		api.HandleFunc("/queue_configs", newCreateQueueConfigHandlerFunc(opts.QueueStore)).Methods("POST")
		api.HandleFunc("/queue_configs/{name}", newGetQueueConfigHandlerFunc(opts.QueueStore)).Methods("GET")
		api.HandleFunc("/queue_configs/{name}", newUpdateQueueConfigHandlerFunc(opts.QueueStore)).Methods("PUT")
		api.HandleFunc("/queue_configs/{name}", newDeleteQueueConfigHandlerFunc(opts.QueueStore)).Methods("DELETE")
		api.HandleFunc("/queue_configs/{name}:restore", newRestoreQueueConfigHandlerFunc(opts.QueueStore)).Methods("POST")
	}

	// Time series metrics endpoints.
	api.HandleFunc("/metrics", newGetMetricsHandlerFunc(http.DefaultClient, opts.PrometheusAddress)).Methods("GET")

	// Restrict APIs when running in read-only mode.
	if opts.ReadOnly {
		api.Use(restrictToReadOnly)
	}

	// Apply auth middleware if AuthStore is configured
	if opts.AuthStore != nil {
		api.Use(authMiddleware)
	}

	// Everything else, route to uiAssetsHandler.
	router.NotFoundHandler = &uiAssetsHandler{
		rootPath:       opts.RootPath,
		contents:       staticContents,
		staticDirPath:  "ui/build",
		indexFileName:  "index.html",
		prometheusAddr: opts.PrometheusAddress,
		readOnly:       opts.ReadOnly,
	}

	return router
}

// restrictToReadOnly is a middleware function to restrict users to perform only GET requests.
func restrictToReadOnly(h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "GET" && r.Method != "" {
			writeJSONError(w, fmt.Sprintf("API Server is running in read-only mode: %s request is not allowed", r.Method), http.StatusMethodNotAllowed)
			return
		}
		h.ServeHTTP(w, r)
	})
}

// authMiddleware is a middleware that requires authentication for all routes
// except the /api/auth/* endpoints.
func authMiddleware(h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Skip auth for /api/auth/* endpoints (using Contains to handle RootPath prefix)
		if strings.Contains(r.URL.Path, "/api/auth/") {
			h.ServeHTTP(w, r)
			return
		}

		// Apply auth middleware
		auth.RequireAuth(h).ServeHTTP(w, r)
	})
}
