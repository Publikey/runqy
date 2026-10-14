package queueworker

import (
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"
)

// WorkerQueue is one entry of a worker's heartbeat "queues" field.
type WorkerQueue struct {
	Name     string `json:"name"`
	Priority int    `json:"priority"`
}

// ParseWorkerQueues decodes the "queues" field of a worker heartbeat hash
// (asynq:workers:<id>). Current workers write a JSON object
// ({"inference.high":10,"inference.low":1}); older ones wrote Go's "%v" form
// ("map[inference.high:10 inference.low:1]"). A comma-separated list of names
// is accepted too. Results are sorted by priority (desc), then name.
func ParseWorkerQueues(raw string) []WorkerQueue {
	s := strings.TrimSpace(raw)
	if s == "" {
		return nil
	}

	var out []WorkerQueue
	if strings.HasPrefix(s, "{") {
		var m map[string]int
		if err := json.Unmarshal([]byte(s), &m); err == nil {
			for name, prio := range m {
				out = append(out, WorkerQueue{Name: name, Priority: prio})
			}
		}
	} else {
		s = strings.TrimSuffix(strings.TrimPrefix(s, "map["), "]")
		s = strings.ReplaceAll(s, ",", " ")
		for _, tok := range strings.Fields(s) {
			q := WorkerQueue{Name: tok}
			if i := strings.LastIndex(tok, ":"); i >= 0 {
				q.Name = tok[:i]
				q.Priority, _ = strconv.Atoi(tok[i+1:])
			}
			if q.Name != "" {
				out = append(out, q)
			}
		}
	}

	sort.Slice(out, func(i, j int) bool {
		if out[i].Priority != out[j].Priority {
			return out[i].Priority > out[j].Priority
		}
		return out[i].Name < out[j].Name
	})
	return out
}

// WorkerServesParent reports whether a worker's heartbeat "queues" field
// includes the parent queue itself or any of its sub-queues.
func WorkerServesParent(raw, parent string) bool {
	for _, q := range ParseWorkerQueues(raw) {
		if q.Name == parent {
			return true
		}
		if p, _, _ := ParseQueueName(q.Name); p == parent {
			return true
		}
	}
	return false
}

// FormatWorkerQueues renders a heartbeat "queues" field for humans:
// "inference.high:10, inference.low:1".
func FormatWorkerQueues(raw string) string {
	parsed := ParseWorkerQueues(raw)
	parts := make([]string, len(parsed))
	for i, q := range parsed {
		parts[i] = fmt.Sprintf("%s:%d", q.Name, q.Priority)
	}
	return strings.Join(parts, ", ")
}
