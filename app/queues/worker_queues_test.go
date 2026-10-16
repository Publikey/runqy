package queueworker

import (
	"reflect"
	"testing"
)

func TestParseWorkerQueues(t *testing.T) {
	want := []WorkerQueue{{"inference.high", 10}, {"inference.low", 1}}
	tests := []struct {
		raw  string
		want []WorkerQueue
	}{
		{"", nil},
		{`{"inference.low":1,"inference.high":10}`, want},
		{"map[inference.high:10 inference.low:1]", want},
		{"inference.high,inference.low", []WorkerQueue{{"inference.high", 0}, {"inference.low", 0}}},
		{"{}", nil},
	}
	for _, tt := range tests {
		if got := ParseWorkerQueues(tt.raw); !reflect.DeepEqual(got, tt.want) {
			t.Errorf("ParseWorkerQueues(%q) = %v, want %v", tt.raw, got, tt.want)
		}
	}
}

func TestWorkerServesParent(t *testing.T) {
	tests := []struct {
		raw, parent string
		want        bool
	}{
		{`{"inference.default":5}`, "inference", true},
		{"map[inference.high:10 inference.low:1]", "inference", true},
		{`{"other.default":5}`, "inference", false},
		{"inference", "inference", true},
		{"", "inference", false},
		{"inference.high,inference.low", "inference", true},
		{" inference.high ", "inference", true},
		{"other.default", "inference", false},
		{"map[other.default:5]", "inference", false},
	}
	for _, tt := range tests {
		if got := WorkerServesParent(tt.raw, tt.parent); got != tt.want {
			t.Errorf("WorkerServesParent(%q, %q) = %v, want %v", tt.raw, tt.parent, got, tt.want)
		}
	}
}

func TestFormatWorkerQueues(t *testing.T) {
	if got := FormatWorkerQueues(`{"a.low":1,"a.high":10}`); got != "a.high:10, a.low:1" {
		t.Errorf("got %q", got)
	}
}
