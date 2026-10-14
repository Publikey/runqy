// Package leader provides a best-effort Redis lease used to elect a single runqy server
// for background loops (autoscaler, alert evaluator) when several servers share one Redis.
package leader

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
)

// renewScript extends the lock only if we still own it.
var renewScript = redis.NewScript(`
if redis.call("GET", KEYS[1]) == ARGV[1] then
	return redis.call("PEXPIRE", KEYS[1], ARGV[2])
end
return 0`)

// releaseScript deletes the lock only if we still own it.
var releaseScript = redis.NewScript(`
if redis.call("GET", KEYS[1]) == ARGV[1] then
	return redis.call("DEL", KEYS[1])
end
return 0`)

// Lock is a Redis lease. The holder renews it before each unit of work; the TTL must
// outlive the longest such unit, and is how long a crashed holder takes to be replaced.
// Not safe for concurrent use: each loop owns its Lock.
type Lock struct {
	rdb   *redis.Client
	key   string
	id    string
	ttl   time.Duration
	owned bool
}

// New creates a lock on key with the given lease TTL.
func New(rdb *redis.Client, key string, ttl time.Duration) *Lock {
	return &Lock{rdb: rdb, key: key, id: uuid.NewString(), ttl: ttl}
}

// Acquire takes or renews the lock and reports whether this server holds it.
func (l *Lock) Acquire(ctx context.Context) bool {
	if l.owned {
		n, err := renewScript.Run(ctx, l.rdb, []string{l.key}, l.id, l.ttl.Milliseconds()).Int()
		if err == nil && n == 1 {
			return true
		}
		l.owned = false
	}
	ok, err := l.rdb.SetNX(ctx, l.key, l.id, l.ttl).Result()
	l.owned = err == nil && ok
	return l.owned
}

// Release gives the lock up so another server can take over immediately.
func (l *Lock) Release(ctx context.Context) {
	if !l.owned {
		return
	}
	_ = releaseScript.Run(ctx, l.rdb, []string{l.key}, l.id).Err()
	l.owned = false
}
