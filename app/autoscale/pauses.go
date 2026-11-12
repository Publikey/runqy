package autoscale

import (
	"context"
	"encoding/json"
	"sort"
	"strings"
	"time"

	"github.com/redis/go-redis/v9"
)

// Pause is a queue whose provisioning is currently on hold after repeated failures, as
// persisted by the autoscaler leader (see state.go). It lets the dashboard and the CLI
// explain why a queue with pending work does not scale up.
type Pause struct {
	Queue    string    `json:"queue"`
	Kind     string    `json:"kind"` // provision, boot or host
	Failures int       `json:"failures"`
	Until    time.Time `json:"until"`
	Cause    string    `json:"cause"`
	// Suspended marks a queue that reached max_boot_failures: provisioning is stopped for
	// a day rather than retried within the hour (see ResumeQueue).
	Suspended bool `json:"suspended,omitempty"`
}

// pauseCauses explains each backoff kind in plain words.
var pauseCauses = map[string]string{
	backoffProvision: "the provider refused to rent a machine",
	backoffBoot:      "instances never became ready",
	backoffHost:      "hosts could not start the container",
}

// ListPauses returns the pauses still in effect at now, sorted by queue then kind. It
// reads the leader's persisted state, so any server can answer.
func ListPauses(ctx context.Context, rdb redis.Cmdable, now time.Time) ([]Pause, error) {
	if rdb == nil {
		return nil, nil
	}
	raw, err := rdb.HGetAll(ctx, backoffKey).Result()
	if err != nil {
		return nil, err
	}
	out := make([]Pause, 0, len(raw))
	for field, v := range raw {
		kind, queue, ok := strings.Cut(field, "|")
		if !ok {
			continue
		}
		var b persistedBackoff
		if json.Unmarshal([]byte(v), &b) != nil || !b.Until.After(now) {
			continue
		}
		cause := pauseCauses[kind]
		if b.Suspended {
			cause += "; provisioning suspended"
		}
		out = append(out, Pause{Queue: queue, Kind: kind, Failures: b.Failures, Until: b.Until, Cause: cause, Suspended: b.Suspended})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Queue != out[j].Queue {
			return out[i].Queue < out[j].Queue
		}
		return out[i].Kind < out[j].Kind
	})
	return out, nil
}

// ResumeQueue lifts every provisioning pause of a queue (failure counts start over) and
// returns how many were removed. The autoscaler leader notices on its next evaluation.
func ResumeQueue(ctx context.Context, rdb redis.Cmdable, queue string) (int, error) {
	if rdb == nil {
		return 0, nil
	}
	n, err := rdb.HDel(ctx, backoffKey,
		backoffField(backoffProvision, queue), backoffField(backoffBoot, queue), backoffField(backoffHost, queue)).Result()
	return int(n), err
}
