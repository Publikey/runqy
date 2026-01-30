package cmd

import (
	"context"
	"fmt"
	"os"
	"text/tabwriter"
	"time"

	"github.com/Publikey/runqy/models"
	queueworker "github.com/Publikey/runqy/queues"
	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"
)

var (
	configListDir string

	// config create flags
	createFile       string
	createName       string
	createPriority   int
	createGitURL     string
	createBranch     string
	createStartupCmd string
	createMode       string
	createCodePath   string
	createForce      bool

	// config remove flags
	removeName  string
	removeForce bool
)

// configCmd represents the config command
var configCmd = &cobra.Command{
	Use:   "config",
	Short: "Configuration management commands",
	Long: `Configuration management commands for listing, reloading, and validating queue configs.

Queue configurations are stored in PostgreSQL and loaded from YAML files.

Examples:
  runqy config list
  runqy config reload --dir ./deployment
  runqy config validate --dir ./deployment

Remote mode:
  runqy --server https://runqy.example.com -k API_KEY config list
  runqy --server https://runqy.example.com -k API_KEY config reload`,
}

// configListCmd lists all queue configurations from DB
var configListCmd = &cobra.Command{
	Use:   "list",
	Short: "List all queue configurations from database",
	Long: `List all queue configurations stored in PostgreSQL.

Shows queue name, priority, provider, and deployment info.`,
	RunE: runConfigList,
}

// configReloadCmd reloads configs from YAML files
var configReloadCmd = &cobra.Command{
	Use:   "reload",
	Short: "Reload configurations from YAML files",
	Long: `Reload all queue configurations from YAML files into the database.

This is the same operation as calling POST /workers/reload API.

Note: In remote mode, this triggers reload on the server (--dir is ignored).`,
	RunE: runConfigReload,
}

// configValidateCmd validates YAML files without loading
var configValidateCmd = &cobra.Command{
	Use:   "validate",
	Short: "Validate YAML configuration files",
	Long: `Validate all YAML files in the specified directory without loading them into the database.

Useful for checking configuration syntax before deployment.

Note: This command is local-only and cannot be used in remote mode.`,
	RunE: runConfigValidate,
}

// configCreateCmd creates a new queue configuration
var configCreateCmd = &cobra.Command{
	Use:   "create",
	Short: "Create a new queue configuration",
	Long: `Create a new queue configuration from a YAML file or inline parameters.

Examples:
  # From YAML file (supports both single-queue and multi-queue formats)
  runqy config create -f ./my-queue.yaml

  # Inline parameters
  runqy config create --name myqueue --priority 5 --git-url https://github.com/org/repo.git --startup-cmd "python main.py"

  # Remote mode
  runqy -s https://server:3000 -k API_KEY config create -f ./queue.yaml

YAML File Formats:

Simple single-queue format:
  name: inference_new
  priority: 5
  deployment:
    git_url: "https://github.com/org/worker.git"
    branch: "main"
    startup_cmd: "python main.py"
    mode: "long_running"

Multi-queue format (existing deployment/ folder format):
  queues:
    inference:
      sub_queues:
        - name: high
          priority: 10
        - name: low
          priority: 1
      deployment:
        git_url: "https://github.com/org/worker.git"
        startup_cmd: "python main.py"`,
	RunE: runConfigCreate,
}

// configRemoveCmd removes a queue configuration
var configRemoveCmd = &cobra.Command{
	Use:   "remove [queue_name]",
	Short: "Remove a queue configuration",
	Long: `Remove a queue configuration from the database and all associated Redis data.

By default, removal fails if the queue has pending tasks.
Use --force to remove even if tasks exist.

Examples:
  runqy config remove myqueue
  runqy config remove myqueue --force
  runqy config remove --name myqueue

Remote mode:
  runqy -s https://server:3000 -k API_KEY config remove myqueue`,
	Args: cobra.MaximumNArgs(1),
	RunE: runConfigRemove,
}

// configSyncCmd syncs Redis with PostgreSQL
var configSyncCmd = &cobra.Command{
	Use:   "sync",
	Short: "Sync Redis with PostgreSQL (find/remove orphaned queues)",
	Long: `Audit Redis for queues not in PostgreSQL and optionally clean them up.

By default, performs a dry-run showing orphaned queues.
Use --clean to actually remove orphaned queue data from Redis.
Use --force with --clean to remove even if queues have tasks.

Examples:
  runqy config sync                    # Dry-run: show orphaned queues
  runqy config sync --clean            # Remove empty orphaned queues
  runqy config sync --clean --force    # Remove all orphaned queues

Note: This command is local-only and cannot be used in remote mode.`,
	RunE: runConfigSync,
}

var (
	syncClean bool
	syncForce bool
)

func init() {
	rootCmd.AddCommand(configCmd)
	configCmd.AddCommand(configListCmd)
	configCmd.AddCommand(configReloadCmd)
	configCmd.AddCommand(configValidateCmd)
	configCmd.AddCommand(configCreateCmd)
	configCmd.AddCommand(configRemoveCmd)
	configCmd.AddCommand(configSyncCmd)

	// Reload/Validate flags
	configReloadCmd.Flags().StringVar(&configListDir, "dir", "", "Config directory (defaults to QUEUE_WORKERS_DIR)")
	configValidateCmd.Flags().StringVar(&configListDir, "dir", "", "Config directory (defaults to QUEUE_WORKERS_DIR)")

	// Create flags
	configCreateCmd.Flags().StringVarP(&createFile, "file", "f", "", "YAML config file path")
	configCreateCmd.Flags().StringVar(&createName, "name", "", "Queue name")
	configCreateCmd.Flags().IntVar(&createPriority, "priority", 1, "Queue priority")
	configCreateCmd.Flags().StringVar(&createGitURL, "git-url", "", "Git repository URL")
	configCreateCmd.Flags().StringVar(&createBranch, "branch", "main", "Git branch")
	configCreateCmd.Flags().StringVar(&createStartupCmd, "startup-cmd", "", "Startup command")
	configCreateCmd.Flags().StringVar(&createMode, "mode", "long_running", "Mode: long_running or one_shot")
	configCreateCmd.Flags().StringVar(&createCodePath, "code-path", "", "Path within the repo to the code")
	configCreateCmd.Flags().BoolVar(&createForce, "force", false, "Update existing queue if it already exists")

	// Remove flags
	configRemoveCmd.Flags().StringVar(&removeName, "name", "", "Queue name to remove")
	configRemoveCmd.Flags().BoolVar(&removeForce, "force", false, "Force removal even if queue has pending tasks")

	// Sync flags
	configSyncCmd.Flags().BoolVar(&syncClean, "clean", false, "Actually remove orphaned queues (default is dry-run)")
	configSyncCmd.Flags().BoolVar(&syncForce, "force", false, "Force removal even if queues have tasks")
}

func runConfigList(cmd *cobra.Command, args []string) error {
	// Remote mode: use API client
	if IsRemoteMode() {
		return runConfigListRemote()
	}

	// Local mode: direct DB access
	cfg := GetConfig()

	// Build PostgreSQL connection
	pgDB, err := models.BuildPostgresDB(cfg)
	if err != nil {
		return fmt.Errorf("failed to connect to PostgreSQL: %w", err)
	}
	defer pgDB.Close()

	redisAddr, err := models.BuildRedisConns()
	if err != nil {
		return fmt.Errorf("failed to connect to Redis: %w", err)
	}
	defer redisAddr.RDB.Close()

	store := queueworker.NewStore(pgDB, redisAddr.RDB)
	ctx := context.Background()

	queues, err := store.ListQueues(ctx)
	if err != nil {
		return fmt.Errorf("failed to list queues: %w", err)
	}

	if len(queues) == 0 {
		fmt.Println("No queue configurations found.")
		return nil
	}

	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "NAME\tPRIORITY\tPROVIDER\tMODE\tGIT_URL")

	for _, queueName := range queues {
		queueCfg, err := store.Get(ctx, queueName)
		if err != nil || queueCfg == nil {
			fmt.Fprintf(w, "%s\t-\t-\t-\t-\n", queueName)
			continue
		}

		provider := queueCfg.Provider
		if provider == "" {
			provider = "worker"
		}

		mode := "-"
		gitURL := "-"
		if queueCfg.Deployment != nil {
			mode = queueCfg.Deployment.Mode
			if mode == "" {
				mode = "long_running"
			}
			gitURL = truncate(queueCfg.Deployment.GitURL, 50)
		}

		fmt.Fprintf(w, "%s\t%d\t%s\t%s\t%s\n",
			queueCfg.Name,
			queueCfg.Priority,
			provider,
			mode,
			gitURL,
		)
	}

	w.Flush()
	return nil
}

func runConfigListRemote() error {
	apiClient := NewAPIClient()

	configs, err := apiClient.ListConfigs()
	if err != nil {
		return err
	}

	if len(configs) == 0 {
		fmt.Println("No queue configurations found.")
		return nil
	}

	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "NAME\tPRIORITY\tPROVIDER")

	for _, cfg := range configs {
		provider := cfg.Provider
		if provider == "" {
			provider = "worker"
		}

		fmt.Fprintf(w, "%s\t%d\t%s\n",
			cfg.Name,
			cfg.Priority,
			provider,
		)
	}

	w.Flush()
	return nil
}

func runConfigReload(cmd *cobra.Command, args []string) error {
	// Remote mode: use API client
	if IsRemoteMode() {
		return runConfigReloadRemote()
	}

	// Local mode: direct DB access
	cfg := GetConfig()

	// Override config dir if specified
	dir := cfg.QueueWorkersDir
	if configListDir != "" {
		dir = configListDir
	}

	// Build PostgreSQL connection
	pgDB, err := models.BuildPostgresDB(cfg)
	if err != nil {
		return fmt.Errorf("failed to connect to PostgreSQL: %w", err)
	}
	defer pgDB.Close()

	redisAddr, err := models.BuildRedisConns()
	if err != nil {
		return fmt.Errorf("failed to connect to Redis: %w", err)
	}
	defer redisAddr.RDB.Close()

	store := queueworker.NewStore(pgDB, redisAddr.RDB)
	ctx := context.Background()

	// Load YAML files
	yamls, err := queueworker.LoadAll(dir)
	if err != nil {
		return fmt.Errorf("failed to load YAML files from %s: %w", dir, err)
	}

	if len(yamls) == 0 {
		fmt.Println("No YAML configuration files found.")
		return nil
	}

	var reloaded []string
	var errors []string

	for _, y := range yamls {
		for queueName, queueCfg := range y.Queues {
			configs := queueCfg.ToQueueConfigs(queueName)

			for _, qc := range configs {
				if err := store.Save(ctx, qc); err != nil {
					errors = append(errors, fmt.Sprintf("%s: %v", qc.Name, err))
					continue
				}
				reloaded = append(reloaded, qc.Name)
			}
		}
	}

	// Register queues in asynq
	if len(reloaded) > 0 {
		if err := store.RegisterAsynqQueues(ctx, reloaded); err != nil {
			fmt.Printf("Warning: failed to register asynq queues: %v\n", err)
		}
	}

	fmt.Printf("Reloaded %d queue configuration(s):\n", len(reloaded))
	for _, name := range reloaded {
		fmt.Printf("  - %s\n", name)
	}

	if len(errors) > 0 {
		fmt.Println()
		fmt.Printf("Errors (%d):\n", len(errors))
		for _, e := range errors {
			fmt.Printf("  - %s\n", e)
		}
	}

	return nil
}

func runConfigReloadRemote() error {
	apiClient := NewAPIClient()

	resp, err := apiClient.ReloadConfigs()
	if err != nil {
		return err
	}

	fmt.Printf("Reloaded %d queue configuration(s):\n", len(resp.Reloaded))
	for _, name := range resp.Reloaded {
		fmt.Printf("  - %s\n", name)
	}

	if len(resp.Errors) > 0 {
		fmt.Println()
		fmt.Printf("Errors (%d):\n", len(resp.Errors))
		for _, e := range resp.Errors {
			fmt.Printf("  - %s\n", e)
		}
	}

	return nil
}

func runConfigValidate(cmd *cobra.Command, args []string) error {
	// Validate is local-only - always run locally regardless of saved credentials
	if IsRemoteMode() {
		fmt.Println("Note: config validate runs locally (ignoring remote server settings)")
	}

	cfg := GetConfig()

	// Override config dir if specified
	dir := cfg.QueueWorkersDir
	if configListDir != "" {
		dir = configListDir
	}

	// Load and validate YAML files
	yamls, err := queueworker.LoadAll(dir)
	if err != nil {
		return fmt.Errorf("validation failed: %w", err)
	}

	if len(yamls) == 0 {
		fmt.Println("No YAML configuration files found.")
		return nil
	}

	fmt.Printf("Validated %d YAML file(s) from %s\n", len(yamls), dir)

	totalQueues := 0
	for _, y := range yamls {
		for queueName, queueCfg := range y.Queues {
			configs := queueCfg.ToQueueConfigs(queueName)
			for _, qc := range configs {
				fmt.Printf("  - %s (priority=%d)\n", qc.Name, qc.Priority)
				totalQueues++
			}
		}
	}

	fmt.Printf("\nTotal: %d queue configuration(s)\n", totalQueues)
	fmt.Println("Validation successful!")

	return nil
}

// SimpleQueueYAML represents a simple single-queue YAML format
type SimpleQueueYAML struct {
	Name       string                    `yaml:"name"`
	Priority   int                       `yaml:"priority"`
	Provider   string                    `yaml:"provider,omitempty"`
	Deployment *queueworker.DeploymentYAML `yaml:"deployment,omitempty"`
}

func runConfigCreate(cmd *cobra.Command, args []string) error {
	// Remote mode: use API client
	if IsRemoteMode() {
		return runConfigCreateRemote()
	}
	// Local mode: direct DB access
	return runConfigCreateLocal()
}

func runConfigCreateLocal() error {
	// Build PostgreSQL connection
	cfg := GetConfig()
	pgDB, err := models.BuildPostgresDB(cfg)
	if err != nil {
		return fmt.Errorf("failed to connect to PostgreSQL: %w", err)
	}
	defer pgDB.Close()

	redisAddr, err := models.BuildRedisConns()
	if err != nil {
		return fmt.Errorf("failed to connect to Redis: %w", err)
	}
	defer redisAddr.RDB.Close()

	store := queueworker.NewStore(pgDB, redisAddr.RDB)
	ctx := context.Background()

	var configs []*queueworker.QueueConfig

	if createFile != "" {
		// Load from YAML file
		loadedConfigs, err := loadConfigsFromFile(createFile)
		if err != nil {
			return err
		}
		configs = loadedConfigs
	} else {
		// Build from inline parameters
		if createName == "" {
			return fmt.Errorf("--name is required when not using --file")
		}
		if createGitURL == "" {
			return fmt.Errorf("--git-url is required when not using --file")
		}
		if createStartupCmd == "" {
			return fmt.Errorf("--startup-cmd is required when not using --file")
		}

		now := time.Now().Unix()
		config := &queueworker.QueueConfig{
			Name:     createName,
			Priority: createPriority,
			Deployment: &queueworker.DeploymentConfig{
				GitURL:     createGitURL,
				Branch:     createBranch,
				CodePath:   createCodePath,
				StartupCmd: createStartupCmd,
				Mode:       createMode,
			},
			CreatedAt: now,
			UpdatedAt: now,
		}
		configs = append(configs, config)
	}

	// Check for existing queues if --force is not set
	if !createForce {
		var existingQueues []string
		for _, config := range configs {
			exists, err := store.Exists(ctx, config.Name)
			if err != nil {
				return fmt.Errorf("failed to check if queue '%s' exists: %w", config.Name, err)
			}
			if exists {
				existingQueues = append(existingQueues, config.Name)
			}
		}
		if len(existingQueues) > 0 {
			return fmt.Errorf("queue(s) already exist: %v. Use --force to update existing queues", existingQueues)
		}
	}

	// Save all configs
	var created []string
	var updated []string
	var errors []string

	for _, config := range configs {
		// Check if updating or creating
		exists, _ := store.Exists(ctx, config.Name)

		if err := store.Save(ctx, config); err != nil {
			errors = append(errors, fmt.Sprintf("%s: %v", config.Name, err))
			continue
		}

		if exists {
			updated = append(updated, config.Name)
		} else {
			created = append(created, config.Name)
		}
	}

	// Register queues in asynq
	allQueues := append(created, updated...)
	if len(allQueues) > 0 {
		if err := store.RegisterAsynqQueues(ctx, allQueues); err != nil {
			fmt.Printf("Warning: failed to register asynq queues: %v\n", err)
		}
	}

	if len(created) > 0 {
		fmt.Printf("Created %d queue configuration(s):\n", len(created))
		for _, name := range created {
			fmt.Printf("  - %s\n", name)
		}
	}

	if len(updated) > 0 {
		fmt.Printf("Updated %d queue configuration(s):\n", len(updated))
		for _, name := range updated {
			fmt.Printf("  - %s\n", name)
		}
	}

	if len(errors) > 0 {
		fmt.Println()
		fmt.Printf("Errors (%d):\n", len(errors))
		for _, e := range errors {
			fmt.Printf("  - %s\n", e)
		}
	}

	return nil
}

func runConfigCreateRemote() error {
	apiClient := NewAPIClient()

	if createFile != "" {
		// Load from YAML file and create via API
		configs, err := loadConfigsFromFile(createFile)
		if err != nil {
			return err
		}

		var created []string
		var updated []string
		var errors []string

		for _, config := range configs {
			req := &CreateQueueRequest{
				Name:     config.Name,
				Priority: config.Priority,
				Provider: config.Provider,
			}
			if config.Deployment != nil {
				req.Deployment = &DeploymentConfigAPI{
					GitURL:             config.Deployment.GitURL,
					Branch:             config.Deployment.Branch,
					CodePath:           config.Deployment.CodePath,
					StartupCmd:         config.Deployment.StartupCmd,
					Mode:               config.Deployment.Mode,
					StartupTimeoutSecs: config.Deployment.StartupTimeoutSecs,
					Vaults:             config.Deployment.Vaults,
					GitToken:           config.Deployment.GitToken,
				}
			}

			resp, err := apiClient.CreateQueue(req, createForce)
			if err != nil {
				errors = append(errors, fmt.Sprintf("%s: %v", config.Name, err))
				continue
			}

			if resp.Message == "Queue updated successfully" {
				updated = append(updated, resp.Queue.Name)
			} else {
				created = append(created, resp.Queue.Name)
			}
		}

		if len(created) > 0 {
			fmt.Printf("Created %d queue configuration(s):\n", len(created))
			for _, name := range created {
				fmt.Printf("  - %s\n", name)
			}
		}

		if len(updated) > 0 {
			fmt.Printf("Updated %d queue configuration(s):\n", len(updated))
			for _, name := range updated {
				fmt.Printf("  - %s\n", name)
			}
		}

		if len(errors) > 0 {
			fmt.Println()
			fmt.Printf("Errors (%d):\n", len(errors))
			for _, e := range errors {
				fmt.Printf("  - %s\n", e)
			}
		}

		return nil
	}

	// Build from inline parameters
	if createName == "" {
		return fmt.Errorf("--name is required when not using --file")
	}
	if createGitURL == "" {
		return fmt.Errorf("--git-url is required when not using --file")
	}
	if createStartupCmd == "" {
		return fmt.Errorf("--startup-cmd is required when not using --file")
	}

	req := &CreateQueueRequest{
		Name:     createName,
		Priority: createPriority,
		Deployment: &DeploymentConfigAPI{
			GitURL:     createGitURL,
			Branch:     createBranch,
			CodePath:   createCodePath,
			StartupCmd: createStartupCmd,
			Mode:       createMode,
		},
	}

	resp, err := apiClient.CreateQueue(req, createForce)
	if err != nil {
		return err
	}

	fmt.Printf("%s:\n", resp.Message)
	fmt.Printf("  Name:     %s\n", resp.Queue.Name)
	fmt.Printf("  Priority: %d\n", resp.Queue.Priority)
	if resp.Queue.Deployment != nil {
		fmt.Printf("  Git URL:  %s\n", resp.Queue.Deployment.GitURL)
		fmt.Printf("  Mode:     %s\n", resp.Queue.Deployment.Mode)
	}

	return nil
}

// loadConfigsFromFile loads queue configurations from a YAML file
// Supports both simple single-queue format and multi-queue format
func loadConfigsFromFile(filePath string) ([]*queueworker.QueueConfig, error) {
	data, err := os.ReadFile(filePath)
	if err != nil {
		return nil, fmt.Errorf("failed to read file %s: %w", filePath, err)
	}

	if len(data) == 0 {
		return nil, fmt.Errorf("file %s is empty", filePath)
	}

	// Try multi-queue format first (existing deployment/ folder format)
	var multiQueue queueworker.QueueWorkersYAML
	if err := yaml.Unmarshal(data, &multiQueue); err == nil && multiQueue.Queues != nil && len(multiQueue.Queues) > 0 {
		// Multi-queue format
		var configs []*queueworker.QueueConfig
		for queueName, queueCfg := range multiQueue.Queues {
			queueConfigs := queueCfg.ToQueueConfigs(queueName)
			now := time.Now().Unix()
			for _, cfg := range queueConfigs {
				cfg.CreatedAt = now
				cfg.UpdatedAt = now
			}
			configs = append(configs, queueConfigs...)
		}
		return configs, nil
	}

	// Try simple single-queue format
	var simple SimpleQueueYAML
	if err := yaml.Unmarshal(data, &simple); err != nil {
		return nil, fmt.Errorf("failed to parse YAML: %w", err)
	}

	if simple.Name == "" {
		return nil, fmt.Errorf("invalid YAML format: missing 'name' field or 'queues' map")
	}

	if simple.Priority == 0 {
		simple.Priority = 1 // Default priority
	}

	now := time.Now().Unix()
	config := &queueworker.QueueConfig{
		Name:      simple.Name,
		Priority:  simple.Priority,
		Provider:  simple.Provider,
		CreatedAt: now,
		UpdatedAt: now,
	}

	if simple.Deployment != nil {
		config.Deployment = &queueworker.DeploymentConfig{
			GitURL:             simple.Deployment.GitURL,
			Branch:             simple.Deployment.Branch,
			CodePath:           simple.Deployment.CodePath,
			StartupCmd:         simple.Deployment.StartupCmd,
			Mode:               simple.Deployment.Mode,
			StartupTimeoutSecs: simple.Deployment.StartupTimeoutSecs,
			Vaults:             simple.Deployment.Vaults,
			GitToken:           simple.Deployment.GitToken,
		}
	}

	return []*queueworker.QueueConfig{config}, nil
}

func runConfigRemove(cmd *cobra.Command, args []string) error {
	// Get queue name from args or --name flag
	queueName := removeName
	if len(args) > 0 {
		queueName = args[0]
	}

	if queueName == "" {
		return fmt.Errorf("queue name is required. Usage: runqy config remove <queue_name> or runqy config remove --name <queue_name>")
	}

	// Remote mode: use API client
	if IsRemoteMode() {
		return runConfigRemoveRemote(queueName)
	}
	// Local mode: direct DB access
	return runConfigRemoveLocal(queueName)
}

func runConfigRemoveLocal(queueName string) error {
	// Build PostgreSQL connection
	cfg := GetConfig()
	pgDB, err := models.BuildPostgresDB(cfg)
	if err != nil {
		return fmt.Errorf("failed to connect to PostgreSQL: %w", err)
	}
	defer pgDB.Close()

	redisAddr, err := models.BuildRedisConns()
	if err != nil {
		return fmt.Errorf("failed to connect to Redis: %w", err)
	}
	defer redisAddr.RDB.Close()

	store := queueworker.NewStoreWithAsynq(pgDB, redisAddr.RDB, redisAddr.AsynqOpt)
	ctx := context.Background()

	// Check if queue exists in DB
	exists, err := store.Exists(ctx, queueName)
	if err != nil {
		return fmt.Errorf("failed to check if queue exists: %w", err)
	}

	if !exists {
		return fmt.Errorf("queue '%s' not found", queueName)
	}

	// Check if queue has tasks (unless --force)
	if !removeForce {
		stats, err := store.GetQueueStats(ctx, queueName)
		if err == nil && stats != nil && !stats.IsEmpty {
			return fmt.Errorf("queue '%s' has tasks (pending=%d, active=%d, scheduled=%d, retry=%d). Use --force to delete anyway",
				queueName, stats.Pending, stats.Active, stats.Scheduled, stats.Retry)
		}
	}

	// Delete the queue with full Redis cleanup
	if err := store.DeleteQueueFull(ctx, queueName, removeForce); err != nil {
		return fmt.Errorf("failed to delete queue: %w", err)
	}

	fmt.Printf("Queue '%s' deleted successfully (including all Redis data)\n", queueName)
	return nil
}

func runConfigRemoveRemote(queueName string) error {
	apiClient := NewAPIClient()

	if err := apiClient.DeleteQueue(queueName); err != nil {
		return err
	}

	fmt.Printf("Queue '%s' deleted successfully\n", queueName)
	return nil
}

func runConfigSync(cmd *cobra.Command, args []string) error {
	// Sync is local-only - always run locally regardless of saved credentials
	if IsRemoteMode() {
		fmt.Println("Note: config sync runs locally (ignoring remote server settings)")
	}

	// Build PostgreSQL connection
	cfg := GetConfig()
	pgDB, err := models.BuildPostgresDB(cfg)
	if err != nil {
		return fmt.Errorf("failed to connect to PostgreSQL: %w", err)
	}
	defer pgDB.Close()

	redisAddr, err := models.BuildRedisConns()
	if err != nil {
		return fmt.Errorf("failed to connect to Redis: %w", err)
	}
	defer redisAddr.RDB.Close()

	store := queueworker.NewStoreWithAsynq(pgDB, redisAddr.RDB, redisAddr.AsynqOpt)
	ctx := context.Background()

	if !syncClean {
		// Dry-run: just list orphaned queues
		orphaned, err := store.ListOrphanedQueues(ctx)
		if err != nil {
			return fmt.Errorf("failed to list orphaned queues: %w", err)
		}

		if len(orphaned) == 0 {
			fmt.Println("No orphaned queues found. Redis and PostgreSQL are in sync.")
			return nil
		}

		fmt.Printf("Found %d orphaned queue(s) in Redis (not in PostgreSQL):\n\n", len(orphaned))

		w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
		fmt.Fprintln(w, "QUEUE\tPENDING\tACTIVE\tSCHEDULED\tRETRY\tARCHIVED\tCOMPLETED")

		for _, queueName := range orphaned {
			stats, err := store.GetQueueStats(ctx, queueName)
			if err != nil {
				fmt.Fprintf(w, "%s\t-\t-\t-\t-\t-\t-\n", queueName)
				continue
			}
			fmt.Fprintf(w, "%s\t%d\t%d\t%d\t%d\t%d\t%d\n",
				queueName, stats.Pending, stats.Active, stats.Scheduled, stats.Retry, stats.Archived, stats.Completed)
		}
		w.Flush()

		fmt.Println()
		fmt.Println("Use --clean to remove empty orphaned queues, or --clean --force to remove all.")
		return nil
	}

	// Actually clean up orphaned queues
	result, err := store.CleanupOrphanedQueues(ctx, syncForce)
	if err != nil {
		return fmt.Errorf("failed to cleanup orphaned queues: %w", err)
	}

	if len(result.Orphaned) == 0 {
		fmt.Println("No orphaned queues found. Redis and PostgreSQL are in sync.")
		return nil
	}

	fmt.Printf("Found %d orphaned queue(s):\n", len(result.Orphaned))

	if len(result.Cleaned) > 0 {
		fmt.Printf("\nCleaned %d queue(s):\n", len(result.Cleaned))
		for _, q := range result.Cleaned {
			fmt.Printf("  - %s\n", q)
		}
	}

	if len(result.Skipped) > 0 {
		fmt.Printf("\nSkipped %d non-empty queue(s):\n", len(result.Skipped))
		for _, q := range result.Skipped {
			fmt.Printf("  - %s (use --force to remove)\n", q)
		}
	}

	if len(result.Errors) > 0 {
		fmt.Printf("\nErrors (%d):\n", len(result.Errors))
		for _, e := range result.Errors {
			fmt.Printf("  - %s\n", e)
		}
	}

	return nil
}
