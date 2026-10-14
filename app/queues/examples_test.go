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

// The shipped alerts example must stay loadable and valid.
func TestAlertsExampleIsValid(t *testing.T) {
	cfg, err := LoadFile("../../examples/alerts.yaml")
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if cfg.Alerts == nil || !cfg.Alerts.Enabled || len(cfg.Alerts.Rules) != 4 {
		t.Fatalf("alerts block not loaded: %+v", cfg.Alerts)
	}
	got := EffectiveAlertRules(cfg.Alerts.Rules, cfg.Queues["reports"].Alerts)
	if len(got) != 4 { // no_workers, latency (2m), autoscale_failure, pending; worker_unhealthy off
		t.Fatalf("reports effective rules = %+v", got)
	}
	if len(EffectiveAlertRules(cfg.Alerts.Rules, cfg.Queues["nightly-batch"].Alerts)) != 0 {
		t.Fatal("nightly-batch should be muted")
	}
}
