package autoscale

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/Publikey/runqy/autoscale/provider"
)

const (
	// logsTail is how many lines are requested from the provider.
	logsTail = 200
	// maxSavedLogBytes bounds the logs kept in an instance's metadata (the end is kept).
	maxSavedLogBytes = 16 << 10
	// logsTimeout bounds a provider log fetch (Vast.ai prepares the file asynchronously).
	logsTimeout = 20 * time.Second
)

// ErrLogsUnsupported is returned when the instance's provider cannot return logs.
var ErrLogsUnsupported = errors.New("this provider does not return instance logs")

// InstanceLogs is the answer of FetchInstanceLogs.
type InstanceLogs struct {
	InstanceID string `json:"instance_id"`
	Logs       string `json:"logs"`
	// Source is "live" (just fetched from the provider) or "saved" (captured when the
	// instance was destroyed).
	Source    string `json:"source"`
	FetchedAt string `json:"fetched_at,omitempty"`
	Error     string `json:"error,omitempty"` // why the capture failed, for saved logs
}

// tailBytes keeps the last max bytes of s, cut at a line start when possible.
func tailBytes(s string, max int) string {
	if len(s) <= max {
		return s
	}
	s = s[len(s)-max:]
	for i := 0; i < len(s) && i < 512; i++ {
		if s[i] == '\n' {
			return "[…]\n" + s[i+1:]
		}
	}
	return "[…]" + s
}

// renterID returns the provider's id for the machine, or "" when nothing was rented.
func renterID(inst *Instance) string {
	id, _ := inst.MetadataMap()["provider_instance_id"].(string)
	return id
}

// captureLogs fetches an instance's logs for saving in its metadata before teardown.
// It returns nothing when the provider has no logs API or no machine was rented.
func captureLogs(ctx context.Context, prov provider.Provider, inst *Instance) map[string]string {
	lf, ok := prov.(provider.LogFetcher)
	id := renterID(inst)
	if !ok || id == "" {
		return nil
	}
	lctx, cancel := context.WithTimeout(ctx, logsTimeout)
	defer cancel()
	now := time.Now().UTC().Format(time.RFC3339)
	logs, err := lf.Logs(lctx, id, logsTail)
	if err != nil {
		return map[string]string{"logs_error": err.Error(), "logs_at": now}
	}
	return map[string]string{"logs": tailBytes(logs, maxSavedLogBytes), "logs_at": now}
}

// FetchInstanceLogs returns an instance's logs: fetched live from its provider while the
// machine exists, otherwise those saved when it was destroyed.
func FetchInstanceLogs(ctx context.Context, instances *Store, providers *provider.Store, instanceID string) (*InstanceLogs, error) {
	inst, err := instances.GetByInstanceID(ctx, instanceID)
	if err != nil {
		return nil, err
	}
	if inst == nil {
		return nil, ErrInstanceNotFound
	}
	md := inst.MetadataMap()
	saved := func() *InstanceLogs {
		out := &InstanceLogs{InstanceID: inst.InstanceID, Source: "saved"}
		out.Logs, _ = md["logs"].(string)
		out.FetchedAt, _ = md["logs_at"].(string)
		out.Error, _ = md["logs_error"].(string)
		return out
	}
	if inst.Status == StatusTerminated || inst.Status == StatusFailed {
		return saved(), nil
	}

	id := renterID(inst)
	if id == "" {
		return saved(), nil
	}
	rec, err := providers.Get(ctx, inst.Provider)
	if err != nil {
		return nil, err
	}
	if rec == nil {
		return nil, fmt.Errorf("provider %q not found", inst.Provider)
	}
	prov, err := provider.Build(rec.ProviderType, rec.Config)
	if err != nil {
		return nil, err
	}
	lf, ok := prov.(provider.LogFetcher)
	if !ok {
		return nil, ErrLogsUnsupported
	}
	lctx, cancel := context.WithTimeout(ctx, logsTimeout)
	defer cancel()
	logs, err := lf.Logs(lctx, id, logsTail)
	if err != nil {
		return nil, err
	}
	return &InstanceLogs{
		InstanceID: inst.InstanceID,
		Logs:       tailBytes(logs, maxSavedLogBytes),
		Source:     "live",
		FetchedAt:  time.Now().UTC().Format(time.RFC3339),
	}, nil
}
