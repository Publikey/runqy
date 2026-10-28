package client

import (
	"context"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"

	queueworker "github.com/Publikey/runqy/queues"
	"github.com/Publikey/runqy/third_party/asynq"
)

// The lifecycle limits must be on the task hash as soon as the task is in :pending, i.e.
// written by the enqueue script itself (a worker blocked on BRPOP may pop it immediately).
func TestEnqueueStampsLimitsAtomically(t *testing.T) {
	mr := miniredis.RunT(t)
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	defer rdb.Close()
	ac := asynq.NewClientFromRedisClient(rdb)

	limits := queueworker.ResolvedLimits{
		MaxRetry:       1,
		TTLCompleted:   time.Hour,
		TTLArchived:    2 * time.Hour,
		PendingTimeout: 5 * time.Minute,
		ActiveTimeout:  30 * time.Second,
	}
	info, err := EnqueueGenericTask(ac, rdb, "video.low", 0, map[string]any{"a": 1}, limits)
	if err != nil {
		t.Fatalf("enqueue: %v", err)
	}

	ctx := context.Background()
	if ids, _ := rdb.LRange(ctx, "asynq:{video.low}:pending", 0, -1).Result(); len(ids) != 1 || ids[0] != info.ID {
		t.Fatalf("pending = %v, want [%s]", ids, info.ID)
	}
	got, err := rdb.HGetAll(ctx, "asynq:{video.low}:t:"+info.ID).Result()
	if err != nil {
		t.Fatal(err)
	}
	for field, want := range map[string]string{
		"ttl_completed": "3600", "ttl_archived": "7200", "pending_timeout": "300", "active_timeout": "30",
		"state": "pending",
	} {
		if got[field] != want {
			t.Errorf("%s = %q, want %q", field, got[field], want)
		}
	}
	if got["msg"] == "" || got["pending_since"] == "" {
		t.Errorf("asynq fields missing: %v", got)
	}
}

func TestEnqueueRequestTimeoutOverridesActiveTimeout(t *testing.T) {
	mr := miniredis.RunT(t)
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	defer rdb.Close()
	ac := asynq.NewClientFromRedisClient(rdb)

	info, err := EnqueueGenericTask(ac, rdb, "video", 120, map[string]any{}, queueworker.ResolvedLimits{ActiveTimeout: time.Minute})
	if err != nil {
		t.Fatalf("enqueue: %v", err)
	}
	if v, _ := rdb.HGet(context.Background(), "asynq:{video.default}:t:"+info.ID, "active_timeout").Result(); v != "120" {
		t.Fatalf("active_timeout = %q, want 120", v)
	}
}
