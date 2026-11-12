package health

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/redis/go-redis/v9"
)

func TestWorkerMetricsCollector(t *testing.T) {
	mr := miniredis.RunT(t)
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	defer rdb.Close()
	ctx := context.Background()
	now := time.Now()
	beat := func(id, queues, healthy string, age time.Duration) {
		rdb.HSet(ctx, "asynq:workers:"+id, map[string]interface{}{
			"queues": queues, "healthy": healthy, "status": "running",
			"last_beat": strconv.FormatInt(now.Add(-age).Unix(), 10),
		})
	}
	beat("a:1", `{"inference.high":10,"inference.low":1}`, "true", time.Second)
	beat("b:1", `{"inference.high":10}`, "false", time.Second)    // process down
	beat("c:1", `{"inference":5}`, "true", 2*time.Second)         // parent name: every sub-queue
	beat("old:1", `{"inference.high":10}`, "true", 5*time.Minute) // stale heartbeat
	rdb.RPush(ctx, "asynq:workers:a:1:logs", "not a worker hash") // sub-key, skipped
	queues := []string{"inference.high", "inference.low", "reports.default"}

	c := NewWorkerMetricsCollector(rdb, func() ([]string, error) { return queues, nil })
	want := `
# HELP runqy_queue_unhealthy_workers Number of live workers listening on the queue whose task process is down or degraded.
# TYPE runqy_queue_unhealthy_workers gauge
runqy_queue_unhealthy_workers{queue="inference.high"} 1
runqy_queue_unhealthy_workers{queue="inference.low"} 0
runqy_queue_unhealthy_workers{queue="reports.default"} 0
# HELP runqy_queue_workers Number of live workers (fresh heartbeat) listening on the queue.
# TYPE runqy_queue_workers gauge
runqy_queue_workers{queue="inference.high"} 3
runqy_queue_workers{queue="inference.low"} 2
runqy_queue_workers{queue="reports.default"} 0
# HELP runqy_workers Number of live workers (fresh heartbeat).
# TYPE runqy_workers gauge
runqy_workers 3
`
	if err := testutil.CollectAndCompare(c, strings.NewReader(want)); err != nil {
		t.Fatal(err)
	}
}

// A failed read must leave a gap, not report zero workers (which would page someone).
func TestWorkerMetricsCollectorExportsNothingOnError(t *testing.T) {
	mr := miniredis.RunT(t)
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	defer rdb.Close()

	c := NewWorkerMetricsCollector(rdb, func() ([]string, error) { return nil, errors.New("redis down") })
	if n := testutil.CollectAndCount(c); n != 0 {
		t.Fatalf("exported %d samples although queues could not be listed", n)
	}

	c = NewWorkerMetricsCollector(rdb, func() ([]string, error) { return []string{"q.default"}, nil })
	mr.Close()
	reg := prometheus.NewPedanticRegistry()
	reg.MustRegister(c)
	if mfs, _ := reg.Gather(); len(mfs) != 0 {
		t.Fatalf("exported %d metric families although Redis is down", len(mfs))
	}
}
