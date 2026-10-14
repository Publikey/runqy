package autoscale

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
)

// leaderKey is the Redis lock that elects a single autoscaler when several runqy servers
// share one Redis: only the holder provisions or destroys instances.
const leaderKey = "runqy:autoscale:leader"

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

// leaderLock is a best-effort Redis lease. The TTL spans a few ticks so a crashed
// leader is replaced quickly, while a live one renews it every tick.
type leaderLock struct {
	rdb   *redis.Client
	id    string
	ttl   time.Duration
	owned bool
}

func newLeaderLock(rdb *redis.Client, ttl time.Duration) *leaderLock {
	return &leaderLock{rdb: rdb, id: uuid.NewString(), ttl: ttl}
}

// acquire takes or renews the lock and reports whether this server is the leader.
func (l *leaderLock) acquire(ctx context.Context) bool {
	if l.owned {
		n, err := renewScript.Run(ctx, l.rdb, []string{leaderKey}, l.id, l.ttl.Milliseconds()).Int()
		if err == nil && n == 1 {
			return true
		}
		l.owned = false
	}
	ok, err := l.rdb.SetNX(ctx, leaderKey, l.id, l.ttl).Result()
	l.owned = err == nil && ok
	return l.owned
}

// release gives the lock up so another server can take over immediately.
func (l *leaderLock) release(ctx context.Context) {
	if !l.owned {
		return
	}
	_ = releaseScript.Run(ctx, l.rdb, []string{leaderKey}, l.id).Err()
	l.owned = false
}
