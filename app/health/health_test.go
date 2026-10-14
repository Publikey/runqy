package health

import (
	"context"
	"errors"
	"testing"
	"time"

	queueworker "github.com/Publikey/runqy/queues"
	"github.com/Publikey/runqy/third_party/asynq"
)

func TestParseWorker(t *testing.T) {
	w := ParseWorker("host:1", map[string]string{"last_beat": "100", "active": "2", "last_task_at": "90", "protected": "true", "healthy": "1"})
	if w.Active != 2 || w.LastTaskAt != 90 || !w.Protected || w.LastBeat != 100 || !w.Healthy {
		t.Errorf("got %+v", w)
	}
	old := ParseWorker("host:2", map[string]string{"last_beat": "100", "healthy": "0"})
	if old.Active != -1 {
		t.Errorf("older worker without 'active': got %d, want -1 (unknown)", old.Active)
	}
	if old.Healthy {
		t.Error(`healthy "0" parsed as healthy`)
	}
}

type fakeQueues []queueworker.QueueWithSubQueues

func (f fakeQueues) ListAllQueuesWithSubQueues(context.Context) ([]queueworker.QueueWithSubQueues, error) {
	return f, nil
}

type fakeInspector map[string]*asynq.QueueInfo

func (f fakeInspector) GetQueueInfo(q string) (*asynq.QueueInfo, error) {
	if info, ok := f[q]; ok {
		return info, nil
	}
	return nil, errors.New("queue not found")
}

func TestCollect(t *testing.T) {
	now := time.Unix(1_000_000, 0)
	queues := fakeQueues{
		{Queue: queueworker.Queue{Name: "inference", Enabled: true}, SubQueues: []queueworker.SubQueue{
			{Name: "high", Enabled: true}, {Name: "low", Enabled: true}, {Name: "off", Enabled: false},
		}},
		{Queue: queueworker.Queue{Name: "simple", Enabled: true}, SubQueues: []queueworker.SubQueue{{Name: "default", Enabled: true}}},
		{Queue: queueworker.Queue{Name: "disabled", Enabled: false}},
	}
	insp := fakeInspector{
		"inference.high": {Pending: 2, Active: 1, Latency: 3 * time.Minute},
		"inference.low":  {Pending: 5, Latency: 10 * time.Minute},
		"inference.off":  {Pending: 100},
		"simple.default": {Paused: true},
	}
	workers := []Worker{
		{ID: "a", Queues: `{"inference.high":10}`, LastBeat: now.Unix(), Healthy: true},
		{ID: "b", Queues: `{"inference.low":1}`, LastBeat: now.Unix() - 5, Healthy: false},
		{ID: "stale", Queues: `{"inference.high":10}`, LastBeat: now.Unix() - 120, Healthy: true},
	}
	c := &Collector{queues: queues, inspector: insp, workers: func(context.Context) ([]Worker, error) { return workers, nil }}

	got, err := c.Collect(context.Background(), now)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("got %d queues, want 2 (disabled skipped)", len(got))
	}
	inf := got[0]
	if inf.Name != "inference" || inf.Pending != 7 || inf.Active != 1 || inf.Latency != 10*time.Minute {
		t.Errorf("inference aggregate wrong (disabled sub-queue must be ignored): %+v", inf)
	}
	if inf.Workers != 2 || len(inf.UnhealthyWorkers) != 1 || inf.UnhealthyWorkers[0] != "b" {
		t.Errorf("inference workers wrong: %+v", inf)
	}
	if !got[1].Paused || got[1].Workers != 0 {
		t.Errorf("simple: %+v", got[1])
	}
}

func TestCollectPropagatesWorkerReadError(t *testing.T) {
	c := &Collector{queues: fakeQueues{}, inspector: fakeInspector{}, workers: func(context.Context) ([]Worker, error) {
		return nil, errors.New("redis down")
	}}
	if _, err := c.Collect(context.Background(), time.Now()); err == nil {
		t.Fatal("want error: an outage must not look like 'no workers'")
	}
}
