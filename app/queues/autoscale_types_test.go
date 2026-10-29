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
		{"sub_queues on queue_depth", &AutoscaleConfig{Enabled: true, Provider: "mock", MaxWorkers: 1, ScaleUp: []ScaleTrigger{{Trigger: TriggerQueueDepth, Threshold: 1, SubQueues: []string{"premium"}}}}, false},
		{"sub_queues on schedule rejected", &AutoscaleConfig{Enabled: true, Provider: "mock", MaxWorkers: 1, ScaleUp: []ScaleTrigger{{Trigger: TriggerSchedule, Cron: "0 8 * * *", Workers: 1, SubQueues: []string{"premium"}}}}, true},
		{"sub_queues full name rejected", &AutoscaleConfig{Enabled: true, Provider: "mock", MaxWorkers: 1, ScaleUp: []ScaleTrigger{{Trigger: TriggerNoWorkers, SubQueues: []string{"inference.premium"}}}}, true},
		{"sub_queues duplicate rejected", &AutoscaleConfig{Enabled: true, Provider: "mock", MaxWorkers: 1, ScaleUp: []ScaleTrigger{{Trigger: TriggerNoWorkers, SubQueues: []string{"a", "a"}}}}, true},
		{"negative min_ram_gb", &AutoscaleConfig{Enabled: true, Provider: "mock", MaxWorkers: 1, Instance: &InstanceSpec{MinRAMGB: -1}}, true},
		{"negative min_vcpu", &AutoscaleConfig{Enabled: true, Provider: "mock", MaxWorkers: 1, Instance: &InstanceSpec{MinVCPU: -2}}, true},
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

func TestAutoscaleValidateSubQueues(t *testing.T) {
	asc := &AutoscaleConfig{Enabled: true, Provider: "mock", MaxWorkers: 1,
		ScaleUp: []ScaleTrigger{{Trigger: TriggerQueueDepth, Threshold: 5, SubQueues: []string{"premium"}}}}
	if err := asc.ValidateSubQueues([]string{"premium", "standard"}); err != nil {
		t.Errorf("known sub-queue rejected: %v", err)
	}
	if err := asc.ValidateSubQueues([]string{DefaultSubQueueName}); err == nil {
		t.Error("unknown sub-queue accepted")
	}
}
