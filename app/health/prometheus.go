package health

import (
	"context"
	"log"
	"time"

	queueworker "github.com/Publikey/runqy/queues"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/redis/go-redis/v9"
)

// workerMetricsTimeout bounds one scrape's read of the worker heartbeats.
const workerMetricsTimeout = 5 * time.Second

var (
	queueWorkersDesc = prometheus.NewDesc(
		"runqy_queue_workers",
		"Number of live workers (fresh heartbeat) listening on the queue.",
		[]string{"queue"}, nil,
	)
	queueUnhealthyWorkersDesc = prometheus.NewDesc(
		"runqy_queue_unhealthy_workers",
		"Number of live workers listening on the queue whose task process is down or degraded.",
		[]string{"queue"}, nil,
	)
	workersDesc = prometheus.NewDesc(
		"runqy_workers",
		"Number of live workers (fresh heartbeat).",
		nil, nil,
	)
)

// WorkerMetricsCollector exports how many workers serve each queue. The queue metrics
// (asynq_*) only describe tasks: without this, "jobs are pending and nobody listens"
// cannot be told from a slow queue in Prometheus.
//
// Every known queue gets a sample, so a queue without workers reads 0 rather than
// being absent (an absent series cannot be alerted on with a plain comparison).
type WorkerMetricsCollector struct {
	rdb    *redis.Client
	queues func() ([]string, error) // full queue names, e.g. inference.high
	now    func() time.Time
}

// NewWorkerMetricsCollector creates the collector. queues lists the full queue names
// (asynq.Inspector.Queues).
func NewWorkerMetricsCollector(rdb *redis.Client, queues func() ([]string, error)) *WorkerMetricsCollector {
	return &WorkerMetricsCollector{rdb: rdb, queues: queues, now: time.Now}
}

func (c *WorkerMetricsCollector) Describe(ch chan<- *prometheus.Desc) {
	ch <- queueWorkersDesc
	ch <- queueUnhealthyWorkersDesc
	ch <- workersDesc
}

func (c *WorkerMetricsCollector) Collect(ch chan<- prometheus.Metric) {
	ctx, cancel := context.WithTimeout(context.Background(), workerMetricsTimeout)
	defer cancel()
	// On a failed read nothing is exported: a gap in the series, not a false "0 workers".
	workers, err := ReadWorkers(ctx, c.rdb)
	if err != nil {
		log.Printf("[METRICS] cannot read worker heartbeats: %v", err)
		return
	}
	queues, err := c.queues()
	if err != nil {
		log.Printf("[METRICS] cannot list queues: %v", err)
		return
	}

	now := c.now()
	live := 0
	total := make(map[string]int, len(queues))
	unhealthy := make(map[string]int, len(queues))
	for _, w := range workers {
		if !w.Fresh(now) {
			continue
		}
		live++
		listens := queueworker.ParseWorkerQueues(w.Queues)
		for _, q := range queues {
			if !listensOn(listens, q) {
				continue
			}
			total[q]++
			if !w.Healthy {
				unhealthy[q]++
			}
		}
	}

	ch <- prometheus.MustNewConstMetric(workersDesc, prometheus.GaugeValue, float64(live))
	for _, q := range queues {
		ch <- prometheus.MustNewConstMetric(queueWorkersDesc, prometheus.GaugeValue, float64(total[q]), q)
		ch <- prometheus.MustNewConstMetric(queueUnhealthyWorkersDesc, prometheus.GaugeValue, float64(unhealthy[q]), q)
	}
}

// listensOn reports whether a worker's queue list covers the queue: it names the queue
// itself, or its parent (a worker given a parent name listens on every sub-queue).
func listensOn(listens []queueworker.WorkerQueue, queue string) bool {
	parent, _, _ := queueworker.ParseQueueName(queue)
	for _, l := range listens {
		if l.Name == queue || l.Name == parent {
			return true
		}
	}
	return false
}
