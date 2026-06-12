package queueworker

import (
	"fmt"
	"os"
	"strconv"
	"time"
)

// Validate checks that every set field of a LimitsConfig parses correctly and is non-negative.
// Returns a descriptive error so invalid config is rejected at load/create time rather than
// silently ignored. A nil config is valid (all defaults apply).
func (l *LimitsConfig) Validate() error {
	if l == nil {
		return nil
	}
	if l.MaxRetry != nil && *l.MaxRetry < 0 {
		return fmt.Errorf("limits.max_retry must be >= 0, got %d", *l.MaxRetry)
	}
	for _, f := range []struct{ name, val string }{
		{"ttl_completed", l.TTLCompleted},
		{"ttl_archived", l.TTLArchived},
		{"pending_timeout", l.PendingTimeout},
		{"active_timeout", l.ActiveTimeout},
	} {
		if f.val == "" {
			continue
		}
		d, err := time.ParseDuration(f.val)
		if err != nil {
			return fmt.Errorf("limits.%s: invalid duration %q (use forms like \"24h\", \"90s\", \"0\"): %w", f.name, f.val, err)
		}
		if d < 0 {
			return fmt.Errorf("limits.%s must be >= 0, got %q", f.name, f.val)
		}
	}
	return nil
}

// ResolvedLimits holds the concrete task lifecycle values applied to a task at enqueue.
// They are stamped onto the task so the worker can enforce them per task.
type ResolvedLimits struct {
	MaxRetry       int           // retries before archival
	TTLCompleted   time.Duration // TTL of successfully completed tasks (0 = no expiry)
	TTLArchived    time.Duration // TTL of failed/archived tasks (0 = no expiry)
	PendingTimeout time.Duration // max age in pending before archive (0 = disabled)
	ActiveTimeout  time.Duration // max execution time before retriable timeout (0 = disabled)
}

// ValidateDefaultLimitsEnv checks the server-global RUNQY_DEFAULT_* limit env vars parse correctly.
// Call this at startup so a typo in a default (e.g. "24hh") fails fast instead of being ignored.
func ValidateDefaultLimitsEnv() error {
	if v := os.Getenv("RUNQY_DEFAULT_MAX_RETRY"); v != "" {
		if n, err := strconv.Atoi(v); err != nil || n < 0 {
			return fmt.Errorf("RUNQY_DEFAULT_MAX_RETRY must be an integer >= 0, got %q", v)
		}
	}
	for _, e := range []string{
		"RUNQY_DEFAULT_TTL_COMPLETED", "RUNQY_DEFAULT_TTL_ARCHIVED",
		"RUNQY_DEFAULT_PENDING_TIMEOUT", "RUNQY_DEFAULT_ACTIVE_TIMEOUT",
	} {
		v := os.Getenv(e)
		if v == "" {
			continue
		}
		d, err := time.ParseDuration(v)
		if err != nil {
			return fmt.Errorf("%s: invalid duration %q (use forms like \"24h\", \"90s\", \"0\"): %w", e, v, err)
		}
		if d < 0 {
			return fmt.Errorf("%s must be >= 0, got %q", e, v)
		}
	}
	return nil
}

// DefaultLimits returns the server-global defaults, overridable via environment variables.
// These are the base values a queue's LimitsConfig overrides field-by-field.
func DefaultLimits() ResolvedLimits {
	return ResolvedLimits{
		MaxRetry:       envInt("RUNQY_DEFAULT_MAX_RETRY", 3),
		TTLCompleted:   envDur("RUNQY_DEFAULT_TTL_COMPLETED", 24*time.Hour),
		TTLArchived:    envDur("RUNQY_DEFAULT_TTL_ARCHIVED", 72*time.Hour),
		PendingTimeout: envDur("RUNQY_DEFAULT_PENDING_TIMEOUT", 0),
		ActiveTimeout:  envDur("RUNQY_DEFAULT_ACTIVE_TIMEOUT", 0),
	}
}

// ResolveLimits merges a queue's LimitsConfig (parent-queue override) over the server defaults.
// A nil config or empty field falls back to the default; a present "0" duration disables that
// dimension explicitly.
func ResolveLimits(q *LimitsConfig) ResolvedLimits {
	r := DefaultLimits()
	if q == nil {
		return r
	}
	if q.MaxRetry != nil {
		r.MaxRetry = *q.MaxRetry
	}
	if d, ok := parseDurationField(q.TTLCompleted); ok {
		r.TTLCompleted = d
	}
	if d, ok := parseDurationField(q.TTLArchived); ok {
		r.TTLArchived = d
	}
	if d, ok := parseDurationField(q.PendingTimeout); ok {
		r.PendingTimeout = d
	}
	if d, ok := parseDurationField(q.ActiveTimeout); ok {
		r.ActiveTimeout = d
	}
	return r
}

// ResolveQueueLimits resolves the limits for a queue value (reads Deployment.Limits if present).
func ResolveQueueLimits(q *Queue) ResolvedLimits {
	if q == nil || q.Deployment == nil {
		return DefaultLimits()
	}
	return ResolveLimits(q.Deployment.Limits)
}

// parseDurationField parses a Go duration string. Returns (0,false) when empty/invalid (caller
// keeps its default) and (d,true) when set — including "0" which explicitly disables.
func parseDurationField(s string) (time.Duration, bool) {
	if s == "" {
		return 0, false
	}
	d, err := time.ParseDuration(s)
	if err != nil {
		return 0, false
	}
	return d, true
}

func envInt(key string, def int) int {
	if v := os.Getenv(key); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return def
}

func envDur(key string, def time.Duration) time.Duration {
	if v := os.Getenv(key); v != "" {
		if d, err := time.ParseDuration(v); err == nil {
			return d
		}
	}
	return def
}
