package monitoring

import (
	"encoding/json"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/Publikey/runqy/third_party/asynq"
	"github.com/redis/go-redis/v9"
)

// Requires a local Redis; set PLAYGROUND_TEST_REDIS_PASSWORD (and optionally
// PLAYGROUND_TEST_REDIS_ADDR) to run, otherwise the test is skipped.
func TestPlaygroundEnqueueHandler(t *testing.T) {
	password, ok := os.LookupEnv("PLAYGROUND_TEST_REDIS_PASSWORD")
	if !ok {
		t.Skip("PLAYGROUND_TEST_REDIS_PASSWORD not set; skipping integration test")
	}
	addr := os.Getenv("PLAYGROUND_TEST_REDIS_ADDR")
	if addr == "" {
		addr = "localhost:6379"
	}

	connOpt := asynq.RedisClientOpt{Addr: addr, Password: password}
	asynqClient := asynq.NewClient(connOpt)
	defer asynqClient.Close()
	rdb := redis.NewClient(&redis.Options{Addr: addr, Password: password})
	defer rdb.Close()

	handler := newPlaygroundEnqueueHandlerFunc(asynqClient, rdb, nil)

	t.Run("enqueues count tasks as pending", func(t *testing.T) {
		body := `{"queue":"playground-test","payload":{"hello":"world"},"count":3}`
		req := httptest.NewRequest("POST", "/api/playground/enqueue", strings.NewReader(body))
		rec := httptest.NewRecorder()
		handler(rec, req)

		if rec.Code != 200 {
			t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
		}
		var resp playgroundEnqueueResponse
		if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
			t.Fatalf("invalid response JSON: %v", err)
		}
		if resp.Count != 3 || len(resp.Tasks) != 3 {
			t.Fatalf("expected 3 tasks, got count=%d len=%d", resp.Count, len(resp.Tasks))
		}
		if resp.Tasks[0].Queue != "playground-test.default" {
			t.Errorf("expected queue normalized to playground-test.default, got %q", resp.Tasks[0].Queue)
		}

		ctx := req.Context()
		defer func() {
			for _, task := range resp.Tasks {
				rdb.Del(ctx, "asynq:{playground-test.default}:t:"+task.ID, "asynq:t:"+task.ID)
			}
			rdb.Del(ctx, "asynq:{playground-test.default}:pending")
			rdb.SRem(ctx, "asynq:queues", "playground-test.default")
		}()

		for _, task := range resp.Tasks {
			state, err := rdb.HGet(ctx, "asynq:{playground-test.default}:t:"+task.ID, "state").Result()
			if err != nil || state != "pending" {
				t.Errorf("task %s: expected state=pending, got %q (err=%v)", task.ID, state, err)
			}
		}
		if n, _ := rdb.LLen(ctx, "asynq:{playground-test.default}:pending").Result(); n < 3 {
			t.Errorf("expected >= 3 pending entries, got %d", n)
		}
	})

	t.Run("rejects invalid payload", func(t *testing.T) {
		req := httptest.NewRequest("POST", "/api/playground/enqueue", strings.NewReader(`{"queue":"q","payload":not-json}`))
		rec := httptest.NewRecorder()
		handler(rec, req)
		if rec.Code != 400 {
			t.Errorf("expected 400 for invalid payload, got %d", rec.Code)
		}
	})

	t.Run("rejects missing queue", func(t *testing.T) {
		req := httptest.NewRequest("POST", "/api/playground/enqueue", strings.NewReader(`{"payload":{}}`))
		rec := httptest.NewRecorder()
		handler(rec, req)
		if rec.Code != 400 {
			t.Errorf("expected 400 for missing queue, got %d", rec.Code)
		}
	})

	t.Run("rejects count over limit", func(t *testing.T) {
		req := httptest.NewRequest("POST", "/api/playground/enqueue", strings.NewReader(`{"queue":"q","payload":{},"count":101}`))
		rec := httptest.NewRecorder()
		handler(rec, req)
		if rec.Code != 400 {
			t.Errorf("expected 400 for count > 100, got %d", rec.Code)
		}
	})
}
