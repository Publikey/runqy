package queueworker

import (
	"fmt"
	"net/mail"
	"strings"
	"time"
)

// Alert rule types.
const (
	AlertNoWorkers        = "no_workers"        // no live worker serves the queue
	AlertLatency          = "latency"           // oldest pending job older than max_latency
	AlertPending          = "pending"           // more than threshold jobs pending
	AlertWorkerUnhealthy  = "worker_unhealthy"  // a worker serving the queue reports healthy=false (degraded)
	AlertAutoscaleFailure = "autoscale_failure" // the autoscaler failed to provision an instance
)

// Alert delivery providers.
const (
	AlertProviderSMTP     = "smtp"
	AlertProviderSendGrid = "sendgrid"
)

// DefaultAlertRepeatInterval is how often a still-firing alert is re-sent.
const DefaultAlertRepeatInterval = 4 * time.Hour

// AlertRule is one alert condition. Global rules (AlertSettings.Rules) apply to every
// queue; a queue's own rules replace the global rule of the same type.
type AlertRule struct {
	Type       string `yaml:"type" json:"type"`
	For        string `yaml:"for,omitempty" json:"for,omitempty"`                 // condition must hold this long before firing (Go duration, default 0)
	MaxLatency string `yaml:"max_latency,omitempty" json:"max_latency,omitempty"` // latency
	Threshold  int    `yaml:"threshold,omitempty" json:"threshold,omitempty"`     // pending
	WhenIdle   bool   `yaml:"when_idle,omitempty" json:"when_idle,omitempty"`     // no_workers: also fire when nothing is pending
	Disabled   bool   `yaml:"disabled,omitempty" json:"disabled,omitempty"`       // in a queue override: turn this rule type off
}

// ForDuration returns the rule's hold duration (0 when unset).
func (r AlertRule) ForDuration() time.Duration {
	d, _ := time.ParseDuration(r.For)
	return d
}

// MaxLatencyDuration returns the latency rule threshold (0 when unset).
func (r AlertRule) MaxLatencyDuration() time.Duration {
	d, _ := time.ParseDuration(r.MaxLatency)
	return d
}

// Validate checks one rule.
func (r AlertRule) Validate() error {
	if r.For != "" {
		if d, err := time.ParseDuration(r.For); err != nil || d < 0 {
			return fmt.Errorf("rule %q: invalid for %q", r.Type, r.For)
		}
	}
	switch r.Type {
	case AlertNoWorkers, AlertWorkerUnhealthy, AlertAutoscaleFailure:
	case AlertLatency:
		if r.Disabled {
			return nil
		}
		if d, err := time.ParseDuration(r.MaxLatency); err != nil || d <= 0 {
			return fmt.Errorf("rule %q requires a positive max_latency duration (got %q)", r.Type, r.MaxLatency)
		}
	case AlertPending:
		if !r.Disabled && r.Threshold <= 0 {
			return fmt.Errorf("rule %q requires a positive threshold", r.Type)
		}
	default:
		return fmt.Errorf("unknown alert rule type %q", r.Type)
	}
	return nil
}

func validateRules(rules []AlertRule) error {
	seen := map[string]bool{}
	for i, r := range rules {
		if err := r.Validate(); err != nil {
			return fmt.Errorf("rules[%d]: %w", i, err)
		}
		if seen[r.Type] {
			return fmt.Errorf("rules[%d]: duplicate rule type %q", i, r.Type)
		}
		seen[r.Type] = true
	}
	return nil
}

// QueueAlertsConfig is a queue's alert override, authored as an `alerts` block next to
// `deployment` and stored in the deployment JSON (like autoscale).
type QueueAlertsConfig struct {
	Disabled bool        `yaml:"disabled,omitempty" json:"disabled,omitempty"` // mute every alert for this queue
	Rules    []AlertRule `yaml:"rules,omitempty" json:"rules,omitempty"`
}

// Validate checks a queue override.
func (q *QueueAlertsConfig) Validate() error {
	if q == nil {
		return nil
	}
	if err := validateRules(q.Rules); err != nil {
		return fmt.Errorf("alerts.%w", err)
	}
	return nil
}

// SMTPSettings configures delivery through an SMTP relay.
type SMTPSettings struct {
	Host     string `yaml:"host" json:"host"`
	Port     int    `yaml:"port,omitempty" json:"port,omitempty"` // default 587 (starttls), 465 (tls), 25 (none)
	Username string `yaml:"username,omitempty" json:"username,omitempty"`
	Password string `yaml:"password,omitempty" json:"password,omitempty"` // literal, "vault://vault/key", or empty for RUNQY_SMTP_PASSWORD
	TLS      string `yaml:"tls,omitempty" json:"tls,omitempty"`           // "starttls" (default) | "tls" | "none"
}

// SendGridSettings configures delivery through the SendGrid v3 API.
type SendGridSettings struct {
	APIKey string `yaml:"api_key,omitempty" json:"api_key,omitempty"` // literal, "vault://vault/key", or empty for RUNQY_SENDGRID_API_KEY
}

// AlertSettings is the global alerting configuration: where notifications go and the
// default rules for every queue. It is authored as a top-level `alerts` block in a queue
// YAML file (applied on load/reload) or edited from the dashboard; both write the same
// stored settings.
type AlertSettings struct {
	Enabled        bool              `yaml:"enabled" json:"enabled"`
	Provider       string            `yaml:"provider" json:"provider"` // smtp | sendgrid
	From           string            `yaml:"from" json:"from"`
	Recipients     []string          `yaml:"recipients" json:"recipients"`
	SMTP           *SMTPSettings     `yaml:"smtp,omitempty" json:"smtp,omitempty"`
	SendGrid       *SendGridSettings `yaml:"sendgrid,omitempty" json:"sendgrid,omitempty"`
	Rules          []AlertRule       `yaml:"rules,omitempty" json:"rules,omitempty"`
	RepeatInterval string            `yaml:"repeat_interval,omitempty" json:"repeat_interval,omitempty"` // default 4h; "0" = notify once
	SendResolved   *bool             `yaml:"send_resolved,omitempty" json:"send_resolved,omitempty"`     // default true
}

// RepeatIntervalOrDefault returns how often a still-firing alert is re-sent (0 = never).
func (s *AlertSettings) RepeatIntervalOrDefault() time.Duration {
	if s == nil || s.RepeatInterval == "" {
		return DefaultAlertRepeatInterval
	}
	d, err := time.ParseDuration(s.RepeatInterval)
	if err != nil || d < 0 {
		return DefaultAlertRepeatInterval
	}
	return d
}

// SendResolvedOrDefault reports whether resolution notifications are sent (default true).
func (s *AlertSettings) SendResolvedOrDefault() bool {
	return s == nil || s.SendResolved == nil || *s.SendResolved
}

// Validate checks the settings. A disabled configuration is only checked structurally
// (rules), so it can be saved half-filled.
func (s *AlertSettings) Validate() error {
	if s == nil {
		return nil
	}
	if err := validateRules(s.Rules); err != nil {
		return fmt.Errorf("alerts.%w", err)
	}
	if s.RepeatInterval != "" {
		if d, err := time.ParseDuration(s.RepeatInterval); err != nil || d < 0 {
			return fmt.Errorf("alerts.repeat_interval %q is not a valid duration", s.RepeatInterval)
		}
	}
	for _, r := range s.Recipients {
		if _, err := mail.ParseAddress(r); err != nil {
			return fmt.Errorf("alerts.recipients: invalid address %q", r)
		}
	}
	if s.From != "" {
		if _, err := mail.ParseAddress(s.From); err != nil {
			return fmt.Errorf("alerts.from: invalid address %q", s.From)
		}
	}
	if s.SMTP != nil {
		switch s.SMTP.TLS {
		case "", "starttls", "tls", "none":
		default:
			return fmt.Errorf("alerts.smtp.tls must be starttls, tls or none (got %q)", s.SMTP.TLS)
		}
		if s.SMTP.Port < 0 || s.SMTP.Port > 65535 {
			return fmt.Errorf("alerts.smtp.port %d is out of range", s.SMTP.Port)
		}
	}
	if !s.Enabled {
		return nil
	}
	if len(s.Recipients) == 0 {
		return fmt.Errorf("alerts.recipients: at least one recipient is required when alerts are enabled")
	}
	if s.From == "" {
		return fmt.Errorf("alerts.from is required when alerts are enabled")
	}
	switch s.Provider {
	case AlertProviderSMTP:
		if s.SMTP == nil || strings.TrimSpace(s.SMTP.Host) == "" {
			return fmt.Errorf("alerts.smtp.host is required for provider smtp")
		}
	case AlertProviderSendGrid:
	default:
		return fmt.Errorf("alerts.provider must be %q or %q (got %q)", AlertProviderSMTP, AlertProviderSendGrid, s.Provider)
	}
	return nil
}

// EffectiveAlertRules merges global rules with a queue override: a queue rule replaces
// the global rule of the same type, a disabled queue rule removes it, and a disabled
// queue override mutes everything.
func EffectiveAlertRules(global []AlertRule, q *QueueAlertsConfig) []AlertRule {
	if q != nil && q.Disabled {
		return nil
	}
	byType := map[string]AlertRule{}
	var order []string
	for _, r := range global {
		if _, ok := byType[r.Type]; !ok {
			order = append(order, r.Type)
		}
		byType[r.Type] = r
	}
	if q != nil {
		for _, r := range q.Rules {
			if _, ok := byType[r.Type]; !ok {
				order = append(order, r.Type)
			}
			byType[r.Type] = r
		}
	}
	out := make([]AlertRule, 0, len(order))
	for _, t := range order {
		if r := byType[t]; !r.Disabled {
			out = append(out, r)
		}
	}
	return out
}
