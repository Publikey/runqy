package queueworker

import (
	"fmt"
	"time"

	"github.com/robfig/cron/v3"
)

// Scale trigger type identifiers.
const (
	TriggerNoWorkers = "no_workers"  // jobs pending but zero live workers
	TriggerQueueDepth = "queue_depth" // pending jobs exceed a threshold
	TriggerSchedule   = "schedule"    // cron-based pre-warm / forced shutdown
	TriggerIdle       = "idle"        // worker idle for longer than a timeout

	// Phase 2 (parsed but not yet acted on by the autoscaler).
	TriggerLatency     = "latency"
	TriggerCostBudget  = "cost_budget"
)

// AutoscaleConfig is the per-queue autoscaling configuration. It is carried inside the
// queue's DeploymentConfig (persisted in the deployment JSON column) so it round-trips
// through YAML, the REST API/CLI, and the monitoring UI with a single shared shape.
// In YAML it is authored as a sibling of `deployment` under each queue (see QueueYAML).
type AutoscaleConfig struct {
	Enabled      bool           `yaml:"enabled" json:"enabled"`
	Provider     string         `yaml:"provider" json:"provider"` // references an autoscale_providers row by name
	MinWorkers   int            `yaml:"min_workers" json:"min_workers"`
	MaxWorkers   int            `yaml:"max_workers" json:"max_workers"`
	PollInterval string         `yaml:"poll_interval,omitempty" json:"poll_interval,omitempty"` // Go duration, default 30s
	ScaleUp      []ScaleTrigger `yaml:"scale_up,omitempty" json:"scale_up,omitempty"`
	ScaleDown    []ScaleTrigger `yaml:"scale_down,omitempty" json:"scale_down,omitempty"`
	Instance     *InstanceSpec  `yaml:"instance,omitempty" json:"instance,omitempty"`
}

// ScaleTrigger is one scale-up or scale-down rule. The Trigger field selects the type;
// the remaining fields are interpreted per type (see Validate).
type ScaleTrigger struct {
	Trigger   string `yaml:"trigger" json:"trigger"`
	Threshold int    `yaml:"threshold,omitempty" json:"threshold,omitempty"` // queue_depth
	Cron      string `yaml:"cron,omitempty" json:"cron,omitempty"`           // schedule
	Workers   int    `yaml:"workers,omitempty" json:"workers,omitempty"`     // schedule (target count)
	Timeout   string `yaml:"timeout,omitempty" json:"timeout,omitempty"`     // idle (Go duration)
}

// InstanceSpec describes the GPU instance the provider should provision for this queue.
type InstanceSpec struct {
	GPU             string  `yaml:"gpu,omitempty" json:"gpu,omitempty"`
	Image           string  `yaml:"image,omitempty" json:"image,omitempty"`
	DiskGB          int     `yaml:"disk_gb,omitempty" json:"disk_gb,omitempty"`
	MaxPricePerHour float64 `yaml:"max_price_per_hour,omitempty" json:"max_price_per_hour,omitempty"`
}

// cronParser matches robfig/cron's standard 5-field parser (minute hour dom month dow).
var cronParser = cron.NewParser(cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow)

// PollIntervalOrDefault returns the configured poll interval, or 30s when unset/invalid.
func (a *AutoscaleConfig) PollIntervalOrDefault() time.Duration {
	const def = 30 * time.Second
	if a == nil || a.PollInterval == "" {
		return def
	}
	d, err := time.ParseDuration(a.PollInterval)
	if err != nil || d <= 0 {
		return def
	}
	return d
}

// Validate checks structural validity of an autoscale config. Provider existence is
// resolved separately at runtime (the YAML loader has no DB access).
func (a *AutoscaleConfig) Validate() error {
	if a == nil {
		return nil
	}
	if !a.Enabled {
		return nil // disabled config is not enforced
	}
	if a.Provider == "" {
		return fmt.Errorf("autoscale.provider is required when autoscale is enabled")
	}
	if a.MinWorkers < 0 {
		return fmt.Errorf("autoscale.min_workers cannot be negative")
	}
	if a.MaxWorkers <= 0 {
		return fmt.Errorf("autoscale.max_workers must be greater than 0")
	}
	if a.MaxWorkers < a.MinWorkers {
		return fmt.Errorf("autoscale.max_workers (%d) cannot be less than min_workers (%d)", a.MaxWorkers, a.MinWorkers)
	}
	if a.PollInterval != "" {
		if _, err := time.ParseDuration(a.PollInterval); err != nil {
			return fmt.Errorf("autoscale.poll_interval %q is not a valid duration: %w", a.PollInterval, err)
		}
	}
	for i := range a.ScaleUp {
		if err := a.ScaleUp[i].validate(true); err != nil {
			return fmt.Errorf("autoscale.scale_up[%d]: %w", i, err)
		}
	}
	for i := range a.ScaleDown {
		if err := a.ScaleDown[i].validate(false); err != nil {
			return fmt.Errorf("autoscale.scale_down[%d]: %w", i, err)
		}
	}
	return nil
}

// validate checks a single trigger. scaleUp distinguishes which trigger types are allowed
// on the up vs down side.
func (t *ScaleTrigger) validate(scaleUp bool) error {
	switch t.Trigger {
	case TriggerNoWorkers:
		if !scaleUp {
			return fmt.Errorf("trigger %q is only valid for scale_up", t.Trigger)
		}
	case TriggerQueueDepth:
		if !scaleUp {
			return fmt.Errorf("trigger %q is only valid for scale_up", t.Trigger)
		}
		if t.Threshold <= 0 {
			return fmt.Errorf("trigger %q requires a positive threshold", t.Trigger)
		}
	case TriggerIdle:
		if scaleUp {
			return fmt.Errorf("trigger %q is only valid for scale_down", t.Trigger)
		}
		if t.Timeout == "" {
			return fmt.Errorf("trigger %q requires a timeout", t.Trigger)
		}
		if _, err := time.ParseDuration(t.Timeout); err != nil {
			return fmt.Errorf("trigger %q has invalid timeout %q: %w", t.Trigger, t.Timeout, err)
		}
	case TriggerSchedule:
		if t.Cron == "" {
			return fmt.Errorf("trigger %q requires a cron expression", t.Trigger)
		}
		if _, err := cronParser.Parse(t.Cron); err != nil {
			return fmt.Errorf("trigger %q has invalid cron %q: %w", t.Trigger, t.Cron, err)
		}
		if scaleUp && t.Workers <= 0 {
			return fmt.Errorf("scale_up trigger %q requires a positive workers count", t.Trigger)
		}
	case TriggerLatency, TriggerCostBudget:
		// Phase 2: accepted in config but not yet enforced. Do not fail validation.
	default:
		return fmt.Errorf("unknown trigger type %q", t.Trigger)
	}
	return nil
}

// ParseCron parses a standard 5-field cron expression with the autoscaler's parser.
func ParseCron(expr string) (cron.Schedule, error) {
	return cronParser.Parse(expr)
}
