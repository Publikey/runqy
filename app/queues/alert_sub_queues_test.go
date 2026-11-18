package queueworker

import (
	"strings"
	"testing"
)

// A queue's alert rules may list the sub-queues they watch; unknown names and global rules
// with sub_queues are refused.
func TestAlertRuleSubQueues(t *testing.T) {
	body := func(rule string) string {
		return `queues:
  images:
    sub_queues:
      - name: normal
        priority: 2
      - name: free
        priority: 0` + strictDeployment + `
    alerts:
      rules:
` + rule + "\n"
	}
	pending := func(sub string) string {
		return `        - type: pending
          threshold: 10
          sub_queues: [` + sub + `]`
	}

	if _, err := LoadFile(writeYAML(t, body(pending("normal")))); err != nil {
		t.Fatalf("known sub-queue refused: %v", err)
	}
	for name, rule := range map[string]string{
		"unknown": pending("nope"),
		"wrong type": `        - type: worker_unhealthy
          sub_queues: [normal]`,
	} {
		if _, err := LoadFile(writeYAML(t, body(rule))); err == nil || !strings.Contains(err.Error(), "sub_queues") {
			t.Errorf("%s: err = %v, want a sub_queues error", name, err)
		}
	}

	global := &AlertSettings{Rules: []AlertRule{{Type: AlertPending, Threshold: 10, SubQueues: []string{"normal"}}}}
	if err := global.Validate(); err == nil || !strings.Contains(err.Error(), "sub_queues") {
		t.Errorf("global rule with sub_queues: err = %v, want refused", err)
	}
}
