package queueworker

import (
	"strings"
	"testing"
)

func intPtr(v int) *int { return &v }

// An explicit priority 0 declares an idle-only sub-queue; a missing or negative priority is
// still refused.
func TestLoadFileIdleOnlySubQueue(t *testing.T) {
	sub := func(freePriority string) string {
		return "queues:\n  images:\n    sub_queues:\n      - name: normal\n        priority: 2\n      - name: free\n" +
			freePriority + strictDeployment + "\n"
	}

	cfg, err := LoadFile(writeYAML(t, sub("        priority: 0")))
	if err != nil {
		t.Fatalf("priority 0 refused: %v", err)
	}
	q := cfg.Queues["images"]
	_, subQueues := q.ToQueueAndSubQueues("images")
	byName := map[string]SubQueue{}
	for _, sq := range subQueues {
		byName[sq.Name] = sq
	}
	if !byName["free"].IsIdleOnly() || byName["free"].Priority != 0 {
		t.Errorf("free = %+v, want idle-only priority 0", byName["free"])
	}
	if byName["normal"].IsIdleOnly() || byName["normal"].Priority != 2 {
		t.Errorf("normal = %+v, want priority 2", byName["normal"])
	}
	for _, c := range q.ToQueueConfigs("images") {
		if c.Name == "images.free" && c.Priority != 0 {
			t.Errorf("images.free config priority = %d, want 0", c.Priority)
		}
	}

	for name, body := range map[string]string{
		"missing":  sub(""),
		"negative": sub("        priority: -1"),
	} {
		_, err := LoadFile(writeYAML(t, body))
		if err == nil || !strings.Contains(err.Error(), "priority") {
			t.Errorf("%s priority: err = %v, want a priority error", name, err)
		}
	}
}
