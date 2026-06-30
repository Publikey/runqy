package queueworker

import "testing"

func TestAutoscaleConfigValidate(t *testing.T) {
	tests := []struct {
		name    string
		cfg     *AutoscaleConfig
		wantErr bool
	}{
		{"nil is ok", nil, false},
		{"disabled skips validation", &AutoscaleConfig{Enabled: false, MaxWorkers: 0}, false},
		{
			"valid full config",
			&AutoscaleConfig{
				Enabled: true, Provider: "mock", MinWorkers: 0, MaxWorkers: 3, PollInterval: "30s",
				ScaleUp: []ScaleTrigger{
					{Trigger: TriggerNoWorkers},
					{Trigger: TriggerQueueDepth, Threshold: 5},
					{Trigger: TriggerSchedule, Cron: "0 8 * * 1-5", Workers: 2},
				},
				ScaleDown: []ScaleTrigger{
					{Trigger: TriggerIdle, Timeout: "5m"},
					{Trigger: TriggerSchedule, Cron: "0 20 * * *"},
				},
			},
			false,
		},
		{"missing provider", &AutoscaleConfig{Enabled: true, MaxWorkers: 1}, true},
		{"max < min", &AutoscaleConfig{Enabled: true, Provider: "mock", MinWorkers: 5, MaxWorkers: 1}, true},
		{"max zero", &AutoscaleConfig{Enabled: true, Provider: "mock", MaxWorkers: 0}, true},
		{"bad poll interval", &AutoscaleConfig{Enabled: true, Provider: "mock", MaxWorkers: 1, PollInterval: "nope"}, true},
		{"queue_depth needs threshold", &AutoscaleConfig{Enabled: true, Provider: "mock", MaxWorkers: 1, ScaleUp: []ScaleTrigger{{Trigger: TriggerQueueDepth}}}, true},
		{"idle on scale_up rejected", &AutoscaleConfig{Enabled: true, Provider: "mock", MaxWorkers: 1, ScaleUp: []ScaleTrigger{{Trigger: TriggerIdle, Timeout: "5m"}}}, true},
		{"idle needs timeout", &AutoscaleConfig{Enabled: true, Provider: "mock", MaxWorkers: 1, ScaleDown: []ScaleTrigger{{Trigger: TriggerIdle}}}, true},
		{"bad cron", &AutoscaleConfig{Enabled: true, Provider: "mock", MaxWorkers: 1, ScaleDown: []ScaleTrigger{{Trigger: TriggerSchedule, Cron: "not a cron"}}}, true},
		{"unknown trigger", &AutoscaleConfig{Enabled: true, Provider: "mock", MaxWorkers: 1, ScaleUp: []ScaleTrigger{{Trigger: "wat"}}}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.cfg.Validate()
			if (err != nil) != tt.wantErr {
				t.Fatalf("Validate() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestPollIntervalOrDefault(t *testing.T) {
	if got := (*AutoscaleConfig)(nil).PollIntervalOrDefault(); got.String() != "30s" {
		t.Fatalf("nil default = %v, want 30s", got)
	}
	if got := (&AutoscaleConfig{PollInterval: "bad"}).PollIntervalOrDefault(); got.String() != "30s" {
		t.Fatalf("bad default = %v, want 30s", got)
	}
	if got := (&AutoscaleConfig{PollInterval: "1m"}).PollIntervalOrDefault(); got.String() != "1m0s" {
		t.Fatalf("1m = %v, want 1m0s", got)
	}
}
