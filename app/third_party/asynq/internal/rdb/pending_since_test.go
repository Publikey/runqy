package rdb

import (
	"testing"
	"time"
)

func TestPendingSinceTime(t *testing.T) {
	want := time.Date(2026, 11, 6, 9, 0, 0, 0, time.UTC)
	for name, v := range map[string]int64{
		"nanoseconds":  want.UnixNano(),
		"milliseconds": want.UnixMilli(),
		"seconds":      want.Unix(),
	} {
		if got := pendingSinceTime(v); !got.Equal(want) {
			t.Errorf("%s: pendingSinceTime(%d) = %v, want %v", name, v, got, want)
		}
	}
}
