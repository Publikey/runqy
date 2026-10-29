package queueworker

import (
	"fmt"
	"strings"
	"time"

	"github.com/robfig/cron/v3"
)

// Scale trigger type identifiers.
const (
	TriggerNoWorkers  = "no_workers"  // jobs pending but zero live workers
	TriggerQueueDepth = "queue_depth" // pending jobs exceed a threshold
	TriggerSchedule   = "schedule"    // cron-based pre-warm / forced shutdown
	TriggerIdle       = "idle"        // worker idle for longer than a timeout
	TriggerLatency    = "latency"     // oldest pending job older than max_latency

	// Phase 2 (parsed but not yet acted on by the autoscaler).
	TriggerCostBudget = "cost_budget"
)

// DefaultBootTimeout is how long an instance may take to boot and register a ready
// worker before the autoscaler gives up on it and destroys it.
const DefaultBootTimeout = 20 * time.Minute

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
	BootTimeout  string         `yaml:"boot_timeout,omitempty" json:"boot_timeout,omitempty"`   // Go duration, default 20m
	ScaleUp      []ScaleTrigger `yaml:"scale_up,omitempty" json:"scale_up,omitempty"`
	ScaleDown    []ScaleTrigger `yaml:"scale_down,omitempty" json:"scale_down,omitempty"`
	Instance     *InstanceSpec  `yaml:"instance,omitempty" json:"instance,omitempty"`
}

// ScaleTrigger is one scale-up or scale-down rule. The Trigger field selects the type;
// the remaining fields are interpreted per type (see Validate).
type ScaleTrigger struct {
	Trigger    string `yaml:"trigger" json:"trigger"`
	Threshold  int    `yaml:"threshold,omitempty" json:"threshold,omitempty"`     // queue_depth
	Cron       string `yaml:"cron,omitempty" json:"cron,omitempty"`               // schedule
	Workers    int    `yaml:"workers,omitempty" json:"workers,omitempty"`         // schedule (target count)
	Duration   string `yaml:"duration,omitempty" json:"duration,omitempty"`       // schedule scale_up: hold the target this long (Go duration)
	Timeout    string `yaml:"timeout,omitempty" json:"timeout,omitempty"`         // idle (Go duration)
	MaxLatency string `yaml:"max_latency,omitempty" json:"max_latency,omitempty"` // latency (Go duration)
	// SubQueues limits no_workers, queue_depth and latency to these sub-queues (short
	// names, e.g. ["premium"]). Empty means every sub-queue of the parent.
	SubQueues []string `yaml:"sub_queues,omitempty" json:"sub_queues,omitempty"`
}

// InstanceSpec describes the GPU instance the provider should provision for this queue.
type InstanceSpec struct {
	GPU             string  `yaml:"gpu,omitempty" json:"gpu,omitempty"`
	Image           string  `yaml:"image,omitempty" json:"image,omitempty"`
	DiskGB          int     `yaml:"disk_gb,omitempty" json:"disk_gb,omitempty"`
	MaxPricePerHour float64 `yaml:"max_price_per_hour,omitempty" json:"max_price_per_hour,omitempty"`
	MinRAMGB        int     `yaml:"min_ram_gb,omitempty" json:"min_ram_gb,omitempty"` // system RAM, not GPU memory
	MinVCPU         int     `yaml:"min_vcpu,omitempty" json:"min_vcpu,omitempty"`
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

// BootTimeoutOrDefault returns the configured boot timeout, or DefaultBootTimeout when
// unset/invalid.
func (a *AutoscaleConfig) BootTimeoutOrDefault() time.Duration {
	if a == nil || a.BootTimeout == "" {
		return DefaultBootTimeout
	}
	d, err := time.ParseDuration(a.BootTimeout)
	if err != nil || d <= 0 {
		return DefaultBootTimeout
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
	if a.BootTimeout != "" {
		if d, err := time.ParseDuration(a.BootTimeout); err != nil || d <= 0 {
			return fmt.Errorf("autoscale.boot_timeout %q must be a positive duration", a.BootTimeout)
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
	if in := a.Instance; in != nil {
		if in.MinRAMGB < 0 {
			return fmt.Errorf("autoscale.instance.min_ram_gb cannot be negative")
		}
		if in.MinVCPU < 0 {
			return fmt.Errorf("autoscale.instance.min_vcpu cannot be negative")
		}
	}
	return nil
}

// ValidateSubQueues checks that every trigger's sub_queues names one of the queue's
// sub-queues (short names). Only the YAML loader knows all of them at once; a queue
// without explicit sub-queues has the single sub-queue "default".
func (a *AutoscaleConfig) ValidateSubQueues(known []string) error {
	if a == nil || !a.Enabled {
		return nil
	}
	ok := make(map[string]bool, len(known))
	for _, k := range known {
		ok[k] = true
	}
	for i := range a.ScaleUp {
		for _, sq := range a.ScaleUp[i].SubQueues {
			if !ok[sq] {
				return fmt.Errorf("autoscale.scale_up[%d]: sub_queues: %q is not a sub-queue of this queue (have %v)", i, sq, known)
			}
		}
	}
	return nil
}

// validate checks a single trigger. scaleUp distinguishes which trigger types are allowed
// on the up vs down side.
func (t *ScaleTrigger) validate(scaleUp bool) error {
	if len(t.SubQueues) > 0 {
		switch t.Trigger {
		case TriggerNoWorkers, TriggerQueueDepth, TriggerLatency:
		default:
			return fmt.Errorf("sub_queues is only valid on no_workers, queue_depth and latency triggers")
		}
		seen := make(map[string]bool, len(t.SubQueues))
		for _, sq := range t.SubQueues {
			if sq == "" {
				return fmt.Errorf("sub_queues cannot contain an empty name")
			}
			if strings.Contains(sq, SubQueueSeparator) {
				return fmt.Errorf("sub_queues takes short names (%q, not %q)", sq[strings.LastIndex(sq, SubQueueSeparator)+1:], sq)
			}
			if seen[sq] {
				return fmt.Errorf("sub_queues lists %q twice", sq)
			}
			seen[sq] = true
		}
	}
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
		if t.Duration != "" {
			if !scaleUp {
				return fmt.Errorf("duration is only valid on a scale_up schedule")
			}
			if d, err := time.ParseDuration(t.Duration); err != nil || d <= 0 {
				return fmt.Errorf("trigger %q has invalid duration %q", t.Trigger, t.Duration)
			}
		}
	case TriggerLatency:
		if !scaleUp {
			return fmt.Errorf("trigger %q is only valid for scale_up", t.Trigger)
		}
		if d, err := time.ParseDuration(t.MaxLatency); err != nil || d <= 0 {
			return fmt.Errorf("trigger %q requires a positive max_latency duration (got %q)", t.Trigger, t.MaxLatency)
		}
	case TriggerCostBudget:
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
