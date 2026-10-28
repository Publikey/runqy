package autoscale

import (
	"context"
	"encoding/json"
	"log"
	"strings"
	"time"

	"github.com/redis/go-redis/v9"
)

// Provisioning pauses survive a server restart or a change of leader: they live in a
// Redis hash, field "<kind>|<parent queue>". Without this, a restart would forget that a
// queue's image never boots and immediately rent (and pay for) a new machine.
const backoffKey = "runqy:autoscale:backoff"

// Backoff kinds, one per in-memory map.
const (
	backoffProvision = "provision" // provider errors (no capacity, bad credentials...)
	backoffBoot      = "boot"      // instances that never became ready
	backoffHost      = "host"      // hosts that could not start the container
)

// backoffRetention drops persisted entries whose pause ended this long ago (the queue
// may have been deleted since); a fresh failure would start the count over anyway.
const backoffRetention = 24 * time.Hour

// backoffStore persists provisioning pauses. A nil store keeps them in memory only.
type backoffStore interface {
	LoadAll(ctx context.Context) (map[string]persistedBackoff, error)
	Save(ctx context.Context, field string, b persistedBackoff) error
	Delete(ctx context.Context, field string) error
}

type persistedBackoff struct {
	Failures int       `json:"failures"`
	Until    time.Time `json:"until"`
}

type redisBackoffStore struct{ rdb *redis.Client }

func (s redisBackoffStore) LoadAll(ctx context.Context) (map[string]persistedBackoff, error) {
	raw, err := s.rdb.HGetAll(ctx, backoffKey).Result()
	if err != nil {
		return nil, err
	}
	out := make(map[string]persistedBackoff, len(raw))
	for field, v := range raw {
		var b persistedBackoff
		if json.Unmarshal([]byte(v), &b) == nil {
			out[field] = b
		}
	}
	return out, nil
}

func (s redisBackoffStore) Save(ctx context.Context, field string, b persistedBackoff) error {
	data, err := json.Marshal(b)
	if err != nil {
		return err
	}
	return s.rdb.HSet(ctx, backoffKey, field, data).Err()
}

func (s redisBackoffStore) Delete(ctx context.Context, field string) error {
	return s.rdb.HDel(ctx, backoffKey, field).Err()
}

func backoffField(kind, parent string) string { return kind + "|" + parent }

// backoffMap returns the in-memory map for a kind.
func (a *Autoscaler) backoffMap(kind string) map[string]*provisionBackoff {
	switch kind {
	case backoffBoot:
		return a.bootFails
	case backoffHost:
		return a.hostFails
	default:
		return a.backoff
	}
}

// persistBackoff writes a queue's current pause of the given kind.
func (a *Autoscaler) persistBackoff(kind, parent string) {
	b := a.backoffMap(kind)[parent]
	if a.backoffs == nil || b == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := a.backoffs.Save(ctx, backoffField(kind, parent), persistedBackoff{Failures: b.failures, Until: b.until}); err != nil {
		log.Printf("[AUTOSCALE] queue %q: cannot persist %s backoff: %v", parent, kind, err)
	}
}

// clearBackoff ends a queue's pause of the given kind, in memory and in the store.
func (a *Autoscaler) clearBackoff(kind, parent string) {
	m := a.backoffMap(kind)
	if _, ok := m[parent]; !ok {
		return
	}
	delete(m, parent)
	if a.backoffs == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := a.backoffs.Delete(ctx, backoffField(kind, parent)); err != nil {
		log.Printf("[AUTOSCALE] queue %q: cannot clear %s backoff: %v", parent, kind, err)
	}
}

// loadBackoffs replaces the in-memory pauses with the persisted ones (called when this
// server becomes leader: the previous leader may have recorded failures since).
func (a *Autoscaler) loadBackoffs(ctx context.Context, now time.Time) {
	if a.backoffs == nil {
		return
	}
	all, err := a.backoffs.LoadAll(ctx)
	if err != nil {
		log.Printf("[AUTOSCALE] cannot load persisted backoffs (keeping in-memory state): %v", err)
		return
	}
	a.backoff = make(map[string]*provisionBackoff)
	a.bootFails = make(map[string]*provisionBackoff)
	a.hostFails = make(map[string]*provisionBackoff)
	for field, b := range all {
		kind, parent, ok := strings.Cut(field, "|")
		if !ok || (kind != backoffProvision && kind != backoffBoot && kind != backoffHost) {
			continue
		}
		if now.Sub(b.Until) > backoffRetention {
			_ = a.backoffs.Delete(ctx, field)
			continue
		}
		a.backoffMap(kind)[parent] = &provisionBackoff{failures: b.Failures, until: b.Until}
		if now.Before(b.Until) {
			log.Printf("[AUTOSCALE] queue %q: restored %s backoff (%d failure(s), paused until %s)", parent, kind, b.Failures, b.Until.Format(time.RFC3339))
		}
	}
}

// providerStateTTL bounds how long an idle provider's saved state is kept.
const providerStateTTL = 30 * 24 * time.Hour

// redisProviderState is the provider.StateStore of one configured provider, stored as
// JSON under runqy:autoscale:provider-state:<provider name>.
type redisProviderState struct {
	rdb *redis.Client
	key string
}

func newProviderState(rdb *redis.Client, providerName string) redisProviderState {
	return redisProviderState{rdb: rdb, key: "runqy:autoscale:provider-state:" + providerName}
}

func (s redisProviderState) Load(ctx context.Context, v any) (bool, error) {
	data, err := s.rdb.Get(ctx, s.key).Bytes()
	if err == redis.Nil {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return true, json.Unmarshal(data, v)
}

func (s redisProviderState) Save(ctx context.Context, v any) error {
	data, err := json.Marshal(v)
	if err != nil {
		return err
	}
	return s.rdb.Set(ctx, s.key, data, providerStateTTL).Err()
}
