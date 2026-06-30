package queueworker

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"gopkg.in/yaml.v3"
)

// yamlCache caches the result of LoadAll to avoid re-reading disk on every request.
var yamlCache struct {
	mu      sync.RWMutex
	dir     string
	configs []*QueueWorkersYAML
	loaded  bool
}

// LoadAllCached returns cached YAML configs, loading from disk on first call.
// Use InvalidateYAMLCache() to force a reload (e.g., on hot-reload).
func LoadAllCached(dir string) ([]*QueueWorkersYAML, error) {
	yamlCache.mu.RLock()
	if yamlCache.loaded && yamlCache.dir == dir {
		configs := yamlCache.configs
		yamlCache.mu.RUnlock()
		return configs, nil
	}
	yamlCache.mu.RUnlock()

	// Cache miss — load from disk
	configs, err := LoadAll(dir)
	if err != nil {
		return nil, err
	}

	yamlCache.mu.Lock()
	yamlCache.dir = dir
	yamlCache.configs = configs
	yamlCache.loaded = true
	yamlCache.mu.Unlock()

	return configs, nil
}

// InvalidateYAMLCache clears the cache so the next LoadAllCached call re-reads disk.
func InvalidateYAMLCache() {
	yamlCache.mu.Lock()
	yamlCache.loaded = false
	yamlCache.configs = nil
	yamlCache.mu.Unlock()
}

// LoadAll loads all YAML files from the specified directory
func LoadAll(dir string) ([]*QueueWorkersYAML, error) {
	// Check if directory exists
	info, err := os.Stat(dir)
	if os.IsNotExist(err) {
		return nil, fmt.Errorf("queueworkers directory not found: %s", dir)
	}
	if err != nil {
		return nil, fmt.Errorf("failed to stat directory %s: %w", dir, err)
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("%s is not a directory", dir)
	}

	// Find all YAML files
	files, err := filepath.Glob(filepath.Join(dir, "*.yaml"))
	if err != nil {
		return nil, fmt.Errorf("failed to glob yaml files: %w", err)
	}

	// Also check for .yml files
	ymlFiles, err := filepath.Glob(filepath.Join(dir, "*.yml"))
	if err != nil {
		return nil, fmt.Errorf("failed to glob yml files: %w", err)
	}
	files = append(files, ymlFiles...)

	if len(files) == 0 {
		return nil, nil // No files found, not an error
	}

	var configs []*QueueWorkersYAML
	for _, file := range files {
		cfg, err := LoadFile(file)
		if err != nil {
			return nil, fmt.Errorf("failed to load %s: %w", file, err)
		}
		if cfg != nil {
			configs = append(configs, cfg)
		}
	}
	return configs, nil
}

// LoadFile loads and validates a single YAML file
func LoadFile(path string) (*QueueWorkersYAML, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read file: %w", err)
	}

	// Skip empty files
	if len(data) == 0 {
		return nil, nil
	}

	var cfg QueueWorkersYAML
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("yaml parse error: %w", err)
	}

	// Skip files that don't have the expected structure
	if cfg.Queues == nil || len(cfg.Queues) == 0 {
		return nil, nil
	}

	// Validate
	if err := cfg.Validate(); err != nil {
		return nil, err
	}

	return &cfg, nil
}

// Validate checks the configuration is valid
func (c *QueueWorkersYAML) Validate() error {
	for queueName, q := range c.Queues {
		if queueName == "" {
			return fmt.Errorf("queue name cannot be empty")
		}

		// Validate queue name doesn't contain separator
		if err := ValidateQueueName(queueName); err != nil {
			return err
		}

		// If no sub_queues, must have priority
		if len(q.SubQueues) == 0 && q.Priority == 0 {
			return fmt.Errorf("queue '%s': either priority or sub_queues is required", queueName)
		}

		// Validate sub_queues if present
		for i, sq := range q.SubQueues {
			if sq.Name == "" {
				return fmt.Errorf("queue '%s': sub_queues[%d].name is required", queueName, i)
			}
			// Validate sub-queue name doesn't contain separator
			if err := ValidateQueueName(sq.Name); err != nil {
				return fmt.Errorf("queue '%s': sub_queues[%d]: %w", queueName, i, err)
			}
			if sq.Priority == 0 {
				return fmt.Errorf("queue '%s': sub_queues[%d].priority is required", queueName, i)
			}
		}

		// Deployment is required for all queues
		if q.Deployment == nil {
			return fmt.Errorf("queue '%s': deployment is required", queueName)
		}
		if q.Deployment.GitURL == "" {
			return fmt.Errorf("queue '%s': deployment.git_url is required", queueName)
		}
		if q.Deployment.StartupCmd == "" {
			return fmt.Errorf("queue '%s': deployment.startup_cmd is required", queueName)
		}
		// Reject malformed task lifecycle limits rather than silently ignoring them.
		if err := q.Deployment.Limits.Validate(); err != nil {
			return fmt.Errorf("queue '%s': deployment.%w", queueName, err)
		}
		// Reject malformed autoscale config rather than silently ignoring it.
		if err := q.Autoscale.Validate(); err != nil {
			return fmt.Errorf("queue '%s': %w", queueName, err)
		}
	}
	return nil
}

// ToQueueAndSubQueues converts a YAML queue config to a Queue and its SubQueues
// The deployment config is stored once in the parent Queue
// Each sub-queue only stores its priority
func (q *QueueYAML) ToQueueAndSubQueues(baseName string) (*Queue, []SubQueue) {
	// Create deployment config if present
	var deployment *DeploymentConfig
	if q.Deployment != nil {
		deployment = &DeploymentConfig{
			GitURL:             q.Deployment.GitURL,
			Branch:             q.Deployment.Branch,
			CodePath:           q.Deployment.CodePath,
			StartupCmd:         q.Deployment.StartupCmd,
			Mode:               q.Deployment.Mode,
			StartupTimeoutSecs: q.Deployment.StartupTimeoutSecs,
			RedisStorage:       q.Deployment.RedisStorage,
			Vaults:             q.Deployment.Vaults,
			GitToken:           q.Deployment.GitToken,
			Limits:             q.Deployment.Limits,
			Autoscale:          q.Autoscale, // fold the sibling autoscale block into the deployment JSON
		}
	}

	// Create the parent queue
	queue := &Queue{
		Name:         baseName,
		Deployment:   deployment,
		InputSchema:  q.Input,
		OutputSchema: q.Output,
		Enabled:      true,
	}

	// Create sub-queues
	var subQueues []SubQueue
	if len(q.SubQueues) > 0 {
		for _, sq := range q.SubQueues {
			subQueues = append(subQueues, SubQueue{
				Name:     sq.Name,
				Priority: sq.Priority,
				Enabled:  true,
			})
		}
	} else {
		// No sub-queues defined, create a default one
		subQueues = append(subQueues, SubQueue{
			Name:     DefaultSubQueueName,
			Priority: q.Priority,
			Enabled:  true,
		})
	}

	return queue, subQueues
}

// ToQueueConfigs converts a YAML queue config to runtime QueueConfig(s)
// Deprecated: Use ToQueueAndSubQueues instead
// If sub_queues are defined, creates multiple configs like "inference.high", "inference.low"
// If no sub_queues, creates a single config with name "queueName.default"
func (q *QueueYAML) ToQueueConfigs(baseName string) []*QueueConfig {
	var configs []*QueueConfig

	// Create deployment config if present
	var deployment *DeploymentConfig
	if q.Deployment != nil {
		deployment = &DeploymentConfig{
			GitURL:             q.Deployment.GitURL,
			Branch:             q.Deployment.Branch,
			CodePath:           q.Deployment.CodePath,
			StartupCmd:         q.Deployment.StartupCmd,
			Mode:               q.Deployment.Mode,
			StartupTimeoutSecs: q.Deployment.StartupTimeoutSecs,
			RedisStorage:       q.Deployment.RedisStorage,
			Vaults:             q.Deployment.Vaults,
			GitToken:           q.Deployment.GitToken,
			Limits:             q.Deployment.Limits,
		}
	}

	if len(q.SubQueues) > 0 {
		// Create a config for each sub-queue
		for _, sq := range q.SubQueues {
			cfg := &QueueConfig{
				Name:       BuildFullQueueName(baseName, sq.Name),
				Priority:   sq.Priority,
	
				Deployment: deployment,
			}
			configs = append(configs, cfg)
		}
	} else {
		// No sub-queues, create single config with .default suffix
		cfg := &QueueConfig{
			Name:       BuildFullQueueName(baseName, DefaultSubQueueName),
			Priority:   q.Priority,

			Deployment: deployment,
		}
		configs = append(configs, cfg)
	}

	return configs
}
