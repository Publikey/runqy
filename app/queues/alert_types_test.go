package queueworker

import (
	"strings"
	"testing"
	"time"
)

func TestAlertSettingsValidate(t *testing.T) {
	ok := func() *AlertSettings {
		return &AlertSettings{
			Enabled: true, Provider: AlertProviderSMTP, From: "runqy <alerts@example.com>",
			Recipients: []string{"ops@example.com"}, SMTP: &SMTPSettings{Host: "smtp.example.com"},
			Rules: []AlertRule{{Type: AlertNoWorkers, For: "2m"}, {Type: AlertLatency, MaxLatency: "10m"}},
		}
	}
	if err := ok().Validate(); err != nil {
		t.Fatalf("valid settings rejected: %v", err)
	}

	tests := []struct {
		name   string
		mutate func(*AlertSettings)
		want   string
	}{
		{"no recipients", func(s *AlertSettings) { s.Recipients = nil }, "recipient"},
		{"bad recipient", func(s *AlertSettings) { s.Recipients = []string{"not-an-email"} }, "invalid address"},
		{"no from", func(s *AlertSettings) { s.From = "" }, "from"},
		{"unknown provider", func(s *AlertSettings) { s.Provider = "pigeon" }, "provider"},
		{"smtp without host", func(s *AlertSettings) { s.SMTP = nil }, "smtp.host"},
		{"bad tls", func(s *AlertSettings) { s.SMTP.TLS = "ssl3" }, "tls"},
		{"latency without max", func(s *AlertSettings) { s.Rules = []AlertRule{{Type: AlertLatency}} }, "max_latency"},
		{"pending without threshold", func(s *AlertSettings) { s.Rules = []AlertRule{{Type: AlertPending}} }, "threshold"},
		{"unknown rule", func(s *AlertSettings) { s.Rules = []AlertRule{{Type: "cpu"}} }, "unknown alert rule"},
		{"duplicate rule", func(s *AlertSettings) { s.Rules = append(s.Rules, AlertRule{Type: AlertNoWorkers}) }, "duplicate"},
		{"bad for", func(s *AlertSettings) { s.Rules[0].For = "soon" }, "invalid for"},
		{"bad repeat", func(s *AlertSettings) { s.RepeatInterval = "-1h" }, "repeat_interval"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := ok()
			tt.mutate(s)
			err := s.Validate()
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Errorf("err = %v, want it to mention %q", err, tt.want)
			}
		})
	}

	// A disabled config may be saved incomplete.
	if err := (&AlertSettings{Enabled: false}).Validate(); err != nil {
		t.Errorf("disabled empty settings rejected: %v", err)
	}
	if err := (&AlertSettings{Enabled: true, Provider: AlertProviderSendGrid, From: "a@b.co", Recipients: []string{"c@d.co"}}).Validate(); err != nil {
		t.Errorf("sendgrid settings rejected: %v", err)
	}
}

func TestAlertSettingsDefaults(t *testing.T) {
	var s *AlertSettings
	if s.RepeatIntervalOrDefault() != DefaultAlertRepeatInterval || !s.SendResolvedOrDefault() {
		t.Error("nil settings defaults wrong")
	}
	no := false
	s = &AlertSettings{RepeatInterval: "0", SendResolved: &no}
	if s.RepeatIntervalOrDefault() != 0 || s.SendResolvedOrDefault() {
		t.Error("explicit values not honoured")
	}
	if (AlertRule{For: "90s"}).ForDuration() != 90*time.Second {
		t.Error("ForDuration")
	}
}

func TestEffectiveAlertRules(t *testing.T) {
	global := []AlertRule{
		{Type: AlertNoWorkers, For: "2m"},
		{Type: AlertLatency, MaxLatency: "10m"},
		{Type: AlertWorkerUnhealthy},
	}
	types := func(rs []AlertRule) string {
		var out []string
		for _, r := range rs {
			out = append(out, r.Type+":"+r.MaxLatency)
		}
		return strings.Join(out, ",")
	}

	if got := types(EffectiveAlertRules(global, nil)); got != "no_workers:,latency:10m,worker_unhealthy:" {
		t.Errorf("no override: %s", got)
	}
	override := &QueueAlertsConfig{Rules: []AlertRule{
		{Type: AlertLatency, MaxLatency: "1m"},       // tighter
		{Type: AlertWorkerUnhealthy, Disabled: true}, // off for this queue
		{Type: AlertPending, Threshold: 100},         // queue-only rule
	}}
	if got := types(EffectiveAlertRules(global, override)); got != "no_workers:,latency:1m,pending:" {
		t.Errorf("override: %s", got)
	}
	if got := EffectiveAlertRules(global, &QueueAlertsConfig{Disabled: true}); len(got) != 0 {
		t.Errorf("muted queue still has rules: %v", got)
	}
}

func TestQueueYAMLFoldsAlerts(t *testing.T) {
	q := QueueYAML{
		Priority:   1,
		Deployment: &DeploymentYAML{GitURL: "g", StartupCmd: "s"},
		Alerts:     &QueueAlertsConfig{Rules: []AlertRule{{Type: AlertPending, Threshold: 5}}},
	}
	queue, _ := q.ToQueueAndSubQueues("q")
	if queue.Deployment.Alerts == nil || queue.Deployment.Alerts.Rules[0].Threshold != 5 {
		t.Fatalf("alerts not folded into deployment: %+v", queue.Deployment)
	}
	bad := QueueWorkersYAML{Queues: map[string]QueueYAML{"q": {
		Priority: 1, Deployment: &DeploymentYAML{GitURL: "g", StartupCmd: "s"},
		Alerts: &QueueAlertsConfig{Rules: []AlertRule{{Type: AlertPending}}},
	}}}
	if err := bad.Validate(); err == nil {
		t.Fatal("invalid queue alerts accepted")
	}
}
