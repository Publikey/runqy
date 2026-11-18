package alerts

import (
	"testing"
	"time"

	"github.com/Publikey/runqy/health"
	queueworker "github.com/Publikey/runqy/queues"
)

// A rule listing sub_queues only looks at their pending jobs: the backlog of a sub-queue
// left out (typically an idle-only, priority 0 one) does not fire it.
func TestRuleSubQueuesLimitTheLoad(t *testing.T) {
	q := health.QueueHealth{
		Name: "images", Pending: 900, Latency: time.Hour,
		BySub: map[string]health.SubQueueLoad{
			"paid": {Pending: 0},
			"free": {Pending: 900, Latency: time.Hour},
		},
	}
	rules := []queueworker.AlertRule{
		{Type: queueworker.AlertPending, Threshold: 100},
		{Type: queueworker.AlertLatency, MaxLatency: "10m"},
		{Type: queueworker.AlertNoWorkers},
	}
	for _, r := range rules {
		if active, _, _ := checkRule(r, q); !active {
			t.Errorf("%s over the whole queue: not active", r.Type)
		}
		r.SubQueues = []string{"paid"}
		if active, _, _ := checkRule(r, q); active {
			t.Errorf("%s limited to paid: active on the free backlog", r.Type)
		}
		r.SubQueues = []string{"paid", "free"}
		if active, _, _ := checkRule(r, q); !active {
			t.Errorf("%s watching paid and free: not active", r.Type)
		}
	}

	// the selected sub-queue's own load still fires
	q.BySub["paid"] = health.SubQueueLoad{Pending: 150, Latency: 20 * time.Minute}
	for _, r := range rules {
		r.SubQueues = []string{"paid"}
		if active, _, _ := checkRule(r, q); !active {
			t.Errorf("%s limited to paid with a paid backlog: not active", r.Type)
		}
	}
}
