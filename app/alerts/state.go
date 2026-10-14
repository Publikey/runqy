package alerts

import (
	"context"
	"encoding/json"
	"time"

	"github.com/redis/go-redis/v9"
)

// Redis keys. State lives in Redis (not in memory) so a new leader picks up where the
// previous one stopped: no duplicate "firing" email and no lost "resolved" email.
const (
	keyState   = "runqy:alerts:state"   // hash: alert key -> alertState JSON
	keyHistory = "runqy:alerts:history" // list: newest first, capped
	keyEvents  = "runqy:alerts:events"  // list: events recorded by other components (autoscaler)
	keyStatus  = "runqy:alerts:status"  // string: Status JSON

	historyCap = 500
	eventsCap  = 1000
)

// Notification statuses.
const (
	StatusFiring   = "firing"
	StatusResolved = "resolved"
	StatusRepeat   = "repeat" // still firing, re-sent after repeat_interval
	StatusEvent    = "event"  // one-off event (autoscale failure)
)

// alertState tracks one (queue, rule) condition.
type alertState struct {
	Queue        string    `json:"queue"`
	Rule         string    `json:"rule"`
	Since        time.Time `json:"since"` // when the condition became true
	Firing       bool      `json:"firing"`
	FiredAt      time.Time `json:"fired_at,omitzero"`
	LastNotified time.Time `json:"last_notified,omitzero"`
	Message      string    `json:"message"`
}

// HistoryEntry is one sent notification.
type HistoryEntry struct {
	Time    time.Time `json:"time"`
	Queue   string    `json:"queue"`
	Rule    string    `json:"rule"`
	Status  string    `json:"status"`
	Message string    `json:"message"`
}

// Event is a one-off occurrence reported by another component, e.g. a failed
// provisioning attempt by the autoscaler.
type Event struct {
	Time    time.Time `json:"time"`
	Queue   string    `json:"queue"`
	Rule    string    `json:"rule"` // rule type that governs it, e.g. autoscale_failure
	Message string    `json:"message"`
}

// Status reports the evaluator's health for the dashboard.
type Status struct {
	LastEval    time.Time `json:"last_eval,omitzero"`
	LastSent    time.Time `json:"last_sent,omitzero"`
	LastError   string    `json:"last_error,omitzero"`
	LastErrorAt time.Time `json:"last_error_at,omitzero"`
}

// stateStore is the persistence the engine needs (Redis in production).
type stateStore interface {
	load(ctx context.Context) (map[string]*alertState, error)
	save(ctx context.Context, upsert map[string]*alertState, remove []string) error
	appendHistory(ctx context.Context, entries []HistoryEntry) error
	drainEvents(ctx context.Context) ([]Event, error)
	getStatus(ctx context.Context) (Status, error)
	setStatus(ctx context.Context, s Status) error
}

type redisState struct{ rdb *redis.Client }

func (r redisState) load(ctx context.Context) (map[string]*alertState, error) {
	raw, err := r.rdb.HGetAll(ctx, keyState).Result()
	if err != nil {
		return nil, err
	}
	out := make(map[string]*alertState, len(raw))
	for k, v := range raw {
		var st alertState
		if json.Unmarshal([]byte(v), &st) == nil {
			out[k] = &st
		}
	}
	return out, nil
}

func (r redisState) save(ctx context.Context, upsert map[string]*alertState, remove []string) error {
	_, err := r.rdb.TxPipelined(ctx, func(p redis.Pipeliner) error {
		if len(upsert) > 0 {
			fields := make(map[string]interface{}, len(upsert))
			for k, st := range upsert {
				b, _ := json.Marshal(st)
				fields[k] = string(b)
			}
			p.HSet(ctx, keyState, fields)
		}
		if len(remove) > 0 {
			p.HDel(ctx, keyState, remove...)
		}
		return nil
	})
	return err
}

func (r redisState) appendHistory(ctx context.Context, entries []HistoryEntry) error {
	if len(entries) == 0 {
		return nil
	}
	vals := make([]interface{}, len(entries))
	for i, e := range entries {
		b, _ := json.Marshal(e)
		vals[i] = string(b)
	}
	_, err := r.rdb.TxPipelined(ctx, func(p redis.Pipeliner) error {
		p.LPush(ctx, keyHistory, vals...)
		p.LTrim(ctx, keyHistory, 0, historyCap-1)
		return nil
	})
	return err
}

func (r redisState) drainEvents(ctx context.Context) ([]Event, error) {
	var lr *redis.StringSliceCmd
	_, err := r.rdb.TxPipelined(ctx, func(p redis.Pipeliner) error {
		lr = p.LRange(ctx, keyEvents, 0, -1)
		p.Del(ctx, keyEvents)
		return nil
	})
	if err != nil {
		return nil, err
	}
	var out []Event
	for _, v := range lr.Val() {
		var e Event
		if json.Unmarshal([]byte(v), &e) == nil {
			out = append(out, e)
		}
	}
	return out, nil
}

func (r redisState) getStatus(ctx context.Context) (Status, error) {
	var s Status
	b, err := r.rdb.Get(ctx, keyStatus).Bytes()
	if err == redis.Nil {
		return s, nil
	}
	if err != nil {
		return s, err
	}
	_ = json.Unmarshal(b, &s)
	return s, nil
}

func (r redisState) setStatus(ctx context.Context, s Status) error {
	b, _ := json.Marshal(s)
	return r.rdb.Set(ctx, keyStatus, b, 0).Err()
}

// History returns the most recent notifications, newest first.
func History(ctx context.Context, rdb *redis.Client, limit int64) ([]HistoryEntry, error) {
	if limit <= 0 || limit > historyCap {
		limit = historyCap
	}
	raw, err := rdb.LRange(ctx, keyHistory, 0, limit-1).Result()
	if err != nil {
		return nil, err
	}
	out := make([]HistoryEntry, 0, len(raw))
	for _, v := range raw {
		var e HistoryEntry
		if json.Unmarshal([]byte(v), &e) == nil {
			out = append(out, e)
		}
	}
	return out, nil
}

// EventRecorder lets other components (the autoscaler) report one-off events. Events
// go through Redis so they reach the evaluator even if it runs on another server.
type EventRecorder struct{ rdb *redis.Client }

// NewEventRecorder creates a recorder.
func NewEventRecorder(rdb *redis.Client) *EventRecorder { return &EventRecorder{rdb: rdb} }

// Record queues an event for the next evaluation. Errors are returned but callers
// usually just log them: alerting must never break the component reporting.
func (r *EventRecorder) Record(ctx context.Context, queue, rule, message string) error {
	b, _ := json.Marshal(Event{Time: time.Now(), Queue: queue, Rule: rule, Message: message})
	_, err := r.rdb.TxPipelined(ctx, func(p redis.Pipeliner) error {
		p.RPush(ctx, keyEvents, string(b))
		p.LTrim(ctx, keyEvents, -eventsCap, -1)
		return nil
	})
	return err
}
