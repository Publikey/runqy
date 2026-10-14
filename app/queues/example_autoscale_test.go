package queueworker

import "testing"

// The shipped autoscale example must stay loadable and valid.
func TestAutoscaleExampleIsValid(t *testing.T) {
	cfg, err := LoadFile("../../examples/autoscale.yaml")
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if err := cfg.Validate(); err != nil {
		t.Fatalf("validate: %v", err)
	}
	qy := cfg.Queues["asdemo"]
	q, _ := qy.ToQueueAndSubQueues("asdemo")
	asc := q.Deployment.Autoscale
	if asc == nil || !asc.Enabled || len(asc.ScaleUp) != 4 || asc.BootTimeout != "20m" {
		t.Fatalf("autoscale block not loaded as expected: %+v", asc)
	}
}
