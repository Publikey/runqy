package alerts

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/Publikey/runqy/health"
	queueworker "github.com/Publikey/runqy/queues"
)

// ---- fakes ----

type memState struct {
	states  map[string]*alertState
	history []HistoryEntry
	events  []Event
	status  Status
}

func newMemState() *memState { return &memState{states: map[string]*alertState{}} }

func (m *memState) load(context.Context) (map[string]*alertState, error) {
	out := map[string]*alertState{}
	for k, v := range m.states {
		cp := *v
		out[k] = &cp
	}
	return out, nil
}

func (m *memState) save(_ context.Context, upsert map[string]*alertState, remove []string) error {
	for k, v := range upsert {
		cp := *v
		m.states[k] = &cp
	}
	for _, k := range remove {
		delete(m.states, k)
	}
	return nil
}

func (m *memState) appendHistory(_ context.Context, e []HistoryEntry) error {
	m.history = append(m.history, e...)
	return nil
}

func (m *memState) drainEvents(context.Context) ([]Event, error) {
	ev := m.events
	m.events = nil
	return ev, nil
}

func (m *memState) getStatus(context.Context) (Status, error) { return m.status, nil }
func (m *memState) setStatus(_ context.Context, s Status) error {
	m.status = s
	return nil
}

type fakeCollector struct{ queues []health.QueueHealth }

func (f *fakeCollector) Collect(context.Context, time.Time) ([]health.QueueHealth, error) {
	return f.queues, nil
}

type fakeSettings struct{ s *queueworker.AlertSettings }

func (f fakeSettings) Get(context.Context) (*queueworker.AlertSettings, error) { return f.s, nil }

type fakeSender struct {
	sent []Email
	err  error
}

func (f *fakeSender) Send(_ context.Context, m Email) error {
	if f.err != nil {
		return f.err
	}
	f.sent = append(f.sent, m)
	return nil
}

func newTestEngine(settings *queueworker.AlertSettings, queues ...health.QueueHealth) (*Engine, *fakeCollector, *memState, *fakeSender) {
	col := &fakeCollector{queues: queues}
	st := newMemState()
	snd := &fakeSender{}
	e := &Engine{
		collector: col,
		settings:  fakeSettings{settings},
		state:     st,
		newSender: func(context.Context, *queueworker.AlertSettings) (Sender, error) { return snd, nil },
		publicURL: "https://runqy.example.com",
	}
	return e, col, st, snd
}

func baseSettings(rules ...queueworker.AlertRule) *queueworker.AlertSettings {
	return &queueworker.AlertSettings{
		Enabled: true, Provider: queueworker.AlertProviderSMTP, From: "runqy <a@example.com>",
		Recipients: []string{"ops@example.com"}, SMTP: &queueworker.SMTPSettings{Host: "smtp"}, Rules: rules,
	}
}

var t0 = time.Date(2026, 10, 14, 12, 0, 0, 0, time.UTC)

func mustEval(t *testing.T, e *Engine, at time.Time) {
	t.Helper()
	if err := e.Evaluate(context.Background(), at); err != nil {
		t.Fatalf("evaluate at %s: %v", at, err)
	}
}

// ---- tests ----

func TestNoWorkersFiresAfterForThenResolves(t *testing.T) {
	settings := baseSettings(queueworker.AlertRule{Type: queueworker.AlertNoWorkers, For: "2m"})
	e, col, st, snd := newTestEngine(settings, health.QueueHealth{Name: "inference", Pending: 3})

	mustEval(t, e, t0)
	mustEval(t, e, t0.Add(time.Minute))
	if len(snd.sent) != 0 {
		t.Fatalf("fired before `for` elapsed: %v", snd.sent)
	}
	mustEval(t, e, t0.Add(2*time.Minute))
	if len(snd.sent) != 1 || !strings.Contains(snd.sent[0].Subject, "FIRING inference: No workers") {
		t.Fatalf("want one firing email, got %+v", snd.sent)
	}
	if !strings.Contains(snd.sent[0].Text, "https://runqy.example.com/monitoring/queues/inference") {
		t.Errorf("email lacks dashboard link:\n%s", snd.sent[0].Text)
	}
	mustEval(t, e, t0.Add(3*time.Minute))
	if len(snd.sent) != 1 {
		t.Fatalf("re-sent while still firing (repeat is 4h): %d emails", len(snd.sent))
	}

	col.queues[0].Workers = 1
	mustEval(t, e, t0.Add(4*time.Minute))
	mustEval(t, e, t0.Add(8*time.Minute))
	if len(snd.sent) != 1 {
		t.Fatalf("resolved before the condition stayed clear for resolve_after (5m): %+v", snd.sent)
	}
	mustEval(t, e, t0.Add(9*time.Minute))
	if len(snd.sent) != 2 || !strings.Contains(snd.sent[1].Subject, "RESOLVED") {
		t.Fatalf("want resolved email, got %+v", snd.sent)
	}
	// The duration is how long the alert really lasted (fired at 2m, clear since 4m).
	if !strings.Contains(snd.sent[1].Text, "resolved after 2m0s") {
		t.Errorf("resolved message should not count the resolve_after delay:\n%s", snd.sent[1].Text)
	}
	if len(st.states) != 0 {
		t.Errorf("state not cleared after resolve: %v", st.states)
	}
	if len(st.history) != 2 {
		t.Errorf("history = %d entries, want 2", len(st.history))
	}
}

func TestNoWorkersIgnoresIdleQueueUnlessWhenIdle(t *testing.T) {
	e, _, _, snd := newTestEngine(baseSettings(queueworker.AlertRule{Type: queueworker.AlertNoWorkers}), health.QueueHealth{Name: "q"})
	mustEval(t, e, t0)
	if len(snd.sent) != 0 {
		t.Fatal("alerted on an empty queue with no workers")
	}
	e, _, _, snd = newTestEngine(baseSettings(queueworker.AlertRule{Type: queueworker.AlertNoWorkers, WhenIdle: true}), health.QueueHealth{Name: "q"})
	mustEval(t, e, t0)
	if len(snd.sent) != 1 {
		t.Fatal("when_idle did not alert")
	}
}

func TestConditionFlappingResetsFor(t *testing.T) {
	settings := baseSettings(queueworker.AlertRule{Type: queueworker.AlertPending, Threshold: 10, For: "5m"})
	e, col, _, snd := newTestEngine(settings, health.QueueHealth{Name: "q", Pending: 50, Workers: 1})
	mustEval(t, e, t0)
	col.queues[0].Pending = 5 // recovers before `for`
	mustEval(t, e, t0.Add(3*time.Minute))
	col.queues[0].Pending = 50
	mustEval(t, e, t0.Add(6*time.Minute)) // true again, but only since 6m
	if len(snd.sent) != 0 {
		t.Fatal("fired although the condition never held for 5m in a row")
	}
	mustEval(t, e, t0.Add(11*time.Minute))
	if len(snd.sent) != 1 {
		t.Fatal("did not fire after holding 5m")
	}
}

// A latency hovering around its threshold (the oldest job is picked up, the next one
// crosses the limit a few minutes later) is one alert, not a firing/resolved pair per swing.
func TestFlappingConditionSendsOneAlert(t *testing.T) {
	settings := baseSettings(queueworker.AlertRule{Type: queueworker.AlertLatency, MaxLatency: "20m"})
	e, col, st, snd := newTestEngine(settings, health.QueueHealth{Name: "q", Pending: 1, Workers: 1, Latency: 21 * time.Minute})
	mustEval(t, e, t0)
	for i := 1; i <= 6; i++ { // clear for 2m, back for 1m, three times over
		col.queues[0].Latency = 19 * time.Minute
		mustEval(t, e, t0.Add(time.Duration(i*3-2)*time.Minute))
		col.queues[0].Latency = 21 * time.Minute
		mustEval(t, e, t0.Add(time.Duration(i*3)*time.Minute))
	}
	if len(snd.sent) != 1 {
		t.Fatalf("flapping sent %d emails, want the single firing one", len(snd.sent))
	}
	if cs := st.states[alertKey("q", queueworker.AlertLatency)].ClearSince; !cs.IsZero() {
		t.Errorf("clear_since = %s after the condition came back, want zero", cs)
	}

	col.queues[0].Latency = time.Minute
	mustEval(t, e, t0.Add(20*time.Minute))
	mustEval(t, e, t0.Add(25*time.Minute))
	if len(snd.sent) != 2 || !strings.Contains(snd.sent[1].Subject, "RESOLVED") {
		t.Fatalf("want one resolved email once clear for 5m, got %d", len(snd.sent))
	}
	if len(st.states) != 0 {
		t.Errorf("state not cleared after resolve: %v", st.states)
	}
}

func TestResolveAfterZeroResolvesAtOnce(t *testing.T) {
	settings := baseSettings(queueworker.AlertRule{Type: queueworker.AlertPending, Threshold: 10, ResolveAfter: "0s"})
	e, col, _, snd := newTestEngine(settings, health.QueueHealth{Name: "q", Pending: 50, Workers: 1})
	mustEval(t, e, t0)
	col.queues[0].Pending = 1
	mustEval(t, e, t0.Add(30*time.Second))
	if len(snd.sent) != 2 || !strings.Contains(snd.sent[1].Subject, "RESOLVED") {
		t.Fatalf("want firing then resolved, got %d emails", len(snd.sent))
	}
}

// A worker reports unhealthy for a few seconds while it stops or restarts its process:
// by default the rule waits a minute before alerting.
func TestWorkerUnhealthyWaitsOneMinuteByDefault(t *testing.T) {
	q := health.QueueHealth{Name: "q", Workers: 1, UnhealthyWorkers: []string{"w1"}}
	e, col, st, snd := newTestEngine(baseSettings(queueworker.AlertRule{Type: queueworker.AlertWorkerUnhealthy}), q)
	mustEval(t, e, t0)
	col.queues[0].UnhealthyWorkers = nil
	mustEval(t, e, t0.Add(30*time.Second))
	if len(snd.sent) != 0 || len(st.states) != 0 {
		t.Fatalf("a 30s blip alerted: %d emails, state %v", len(snd.sent), st.states)
	}
	col.queues[0].UnhealthyWorkers = []string{"w1"}
	mustEval(t, e, t0.Add(time.Minute))
	mustEval(t, e, t0.Add(2*time.Minute))
	if len(snd.sent) != 1 {
		t.Fatalf("unhealthy for 1m did not alert: %d emails", len(snd.sent))
	}

	e, _, _, snd = newTestEngine(baseSettings(queueworker.AlertRule{Type: queueworker.AlertWorkerUnhealthy, For: "0s"}), q)
	mustEval(t, e, t0)
	if len(snd.sent) != 1 {
		t.Fatal(`for: "0s" should alert at once`)
	}
}

func TestRepeatInterval(t *testing.T) {
	settings := baseSettings(queueworker.AlertRule{Type: queueworker.AlertLatency, MaxLatency: "10m"})
	settings.RepeatInterval = "1h"
	e, _, _, snd := newTestEngine(settings, health.QueueHealth{Name: "q", Pending: 1, Workers: 1, Latency: time.Hour})
	mustEval(t, e, t0)
	mustEval(t, e, t0.Add(30*time.Minute))
	mustEval(t, e, t0.Add(61*time.Minute))
	if len(snd.sent) != 2 || !strings.Contains(snd.sent[1].Subject, "STILL FIRING") {
		t.Fatalf("want firing + one repeat, got %d", len(snd.sent))
	}
}

func TestSendFailureRetriesNextEvaluation(t *testing.T) {
	settings := baseSettings(queueworker.AlertRule{Type: queueworker.AlertNoWorkers, ResolveAfter: "0"})
	e, col, st, snd := newTestEngine(settings, health.QueueHealth{Name: "q", Pending: 1})
	snd.err = errors.New("smtp down")
	if err := e.Evaluate(context.Background(), t0); err == nil {
		t.Fatal("want send error")
	}
	if st.status.LastError == "" {
		t.Error("send error not recorded in status")
	}
	snd.err = nil
	mustEval(t, e, t0.Add(30*time.Second))
	if len(snd.sent) != 1 {
		t.Fatalf("firing notification not retried: %d", len(snd.sent))
	}

	// Same for the resolved notification.
	col.queues[0].Workers = 1
	snd.err = errors.New("smtp down")
	_ = e.Evaluate(context.Background(), t0.Add(time.Minute))
	snd.err = nil
	mustEval(t, e, t0.Add(90*time.Second))
	if len(snd.sent) != 2 || !strings.Contains(snd.sent[1].Subject, "RESOLVED") {
		t.Fatalf("resolved notification not retried: %+v", snd.sent)
	}
}

func TestDigestGroupsNotificationsAndQueueOverrides(t *testing.T) {
	settings := baseSettings(
		queueworker.AlertRule{Type: queueworker.AlertNoWorkers},
		queueworker.AlertRule{Type: queueworker.AlertWorkerUnhealthy},
	)
	muted := &queueworker.DeploymentConfig{Alerts: &queueworker.QueueAlertsConfig{Disabled: true}}
	noUnhealthy := &queueworker.DeploymentConfig{Alerts: &queueworker.QueueAlertsConfig{Rules: []queueworker.AlertRule{{Type: queueworker.AlertWorkerUnhealthy, Disabled: true}}}}
	e, _, _, snd := newTestEngine(settings,
		health.QueueHealth{Name: "a", Pending: 1},
		health.QueueHealth{Name: "b", Pending: 1},
		health.QueueHealth{Name: "muted", Pending: 1, Deployment: muted},
		health.QueueHealth{Name: "c", Workers: 1, UnhealthyWorkers: []string{"host:1"}, Deployment: noUnhealthy},
		health.QueueHealth{Name: "paused", Pending: 9, Paused: true},
	)
	mustEval(t, e, t0)
	if len(snd.sent) != 1 {
		t.Fatalf("want one digest email, got %d", len(snd.sent))
	}
	m := snd.sent[0]
	if m.Subject != "[runqy] 2 firing — a, b" {
		t.Errorf("subject = %q", m.Subject)
	}
	for _, unwanted := range []string{"muted", "paused", "host:1"} {
		if strings.Contains(m.Text, unwanted) {
			t.Errorf("digest mentions %q:\n%s", unwanted, m.Text)
		}
	}
}

func TestAutoscaleEventsThrottled(t *testing.T) {
	settings := baseSettings(queueworker.AlertRule{Type: queueworker.AlertAutoscaleFailure})
	settings.RepeatInterval = "1h"
	e, _, st, snd := newTestEngine(settings, health.QueueHealth{Name: "gpu", Workers: 1})

	st.events = []Event{
		{Time: t0, Queue: "gpu", Rule: queueworker.AlertAutoscaleFailure, Message: "no offers"},
		{Time: t0, Queue: "gpu", Rule: queueworker.AlertAutoscaleFailure, Message: "no offers"},
		{Time: t0, Queue: "other", Rule: queueworker.AlertAutoscaleFailure, Message: "unknown queue"},
	}
	mustEval(t, e, t0)
	if len(snd.sent) != 1 || !strings.Contains(snd.sent[0].Text, "no offers (2 occurrences)") {
		t.Fatalf("want one grouped event email, got %+v", snd.sent)
	}

	st.events = []Event{{Time: t0, Queue: "gpu", Rule: queueworker.AlertAutoscaleFailure, Message: "again"}}
	mustEval(t, e, t0.Add(10*time.Minute))
	if len(snd.sent) != 1 {
		t.Fatal("event not throttled within repeat_interval")
	}
	mustEval(t, e, t0.Add(20*time.Minute)) // no new events: throttle entry must survive
	st.events = []Event{{Time: t0, Queue: "gpu", Rule: queueworker.AlertAutoscaleFailure, Message: "again"}}
	mustEval(t, e, t0.Add(30*time.Minute))
	if len(snd.sent) != 1 {
		t.Fatal("throttle entry dropped between evaluations")
	}
	st.events = []Event{{Time: t0, Queue: "gpu", Rule: queueworker.AlertAutoscaleFailure, Message: "again"}}
	mustEval(t, e, t0.Add(61*time.Minute))
	if len(snd.sent) != 2 {
		t.Fatal("event not sent after the throttle window")
	}
}

func TestDisabledSettingsDoNothing(t *testing.T) {
	settings := baseSettings(queueworker.AlertRule{Type: queueworker.AlertNoWorkers})
	settings.Enabled = false
	e, _, st, snd := newTestEngine(settings, health.QueueHealth{Name: "q", Pending: 1})
	mustEval(t, e, t0)
	if len(snd.sent) != 0 || len(st.states) != 0 {
		t.Fatal("disabled alerting evaluated rules")
	}
}

func TestDigestFooterMentionsRemindersOnlyForFiringAlerts(t *testing.T) {
	settings := baseSettings()
	firing := renderDigest(settings, []HistoryEntry{{Queue: "q", Rule: queueworker.AlertNoWorkers, Status: StatusFiring}}, "", t0)
	if !strings.Contains(firing.Text, "re-sent every 4h") {
		t.Errorf("firing email lacks the reminder line:\n%s", firing.Text)
	}
	for _, status := range []string{StatusEvent, StatusResolved} {
		m := renderDigest(settings, []HistoryEntry{{Queue: "q", Rule: queueworker.AlertAutoscaleFailure, Status: status}}, "", t0)
		if strings.Contains(m.Text, "re-sent") || strings.Contains(m.HTML, "re-sent") {
			t.Errorf("%s email mentions reminders:\n%s", status, m.Text)
		}
	}
}

func TestFiringAlertResolvedWhenNoLongerMonitored(t *testing.T) {
	cases := []struct {
		name   string
		change func(col *fakeCollector)
		reason string
	}{
		{"paused", func(c *fakeCollector) { c.queues[0].Paused = true }, "queue paused"},
		{"muted", func(c *fakeCollector) {
			c.queues[0].Deployment = &queueworker.DeploymentConfig{Alerts: &queueworker.QueueAlertsConfig{Disabled: true}}
		}, "alert muted or rule removed"},
		{"removed", func(c *fakeCollector) { c.queues = nil }, "queue removed or disabled"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			e, col, st, snd := newTestEngine(baseSettings(queueworker.AlertRule{Type: queueworker.AlertNoWorkers}), health.QueueHealth{Name: "q", Pending: 3})
			mustEval(t, e, t0)
			if len(snd.sent) != 1 {
				t.Fatalf("want the firing email first, got %d", len(snd.sent))
			}
			tc.change(col)
			mustEval(t, e, t0.Add(time.Minute))
			if len(snd.sent) != 2 || !strings.Contains(snd.sent[1].Subject, "RESOLVED") || !strings.Contains(snd.sent[1].Text, tc.reason) {
				t.Fatalf("want a RESOLVED email mentioning %q, got %+v", tc.reason, snd.sent)
			}
			if len(st.states) != 0 {
				t.Errorf("state kept after resolve: %v", st.states)
			}
			mustEval(t, e, t0.Add(2*time.Minute))
			if len(snd.sent) != 2 {
				t.Errorf("resolved twice")
			}
		})
	}
}

func TestPendingAlertDroppedSilentlyWhenNoLongerMonitored(t *testing.T) {
	e, col, st, snd := newTestEngine(baseSettings(queueworker.AlertRule{Type: queueworker.AlertNoWorkers, For: "5m"}), health.QueueHealth{Name: "q", Pending: 3})
	mustEval(t, e, t0) // condition true but not firing yet
	col.queues[0].Paused = true
	mustEval(t, e, t0.Add(time.Minute))
	if len(snd.sent) != 0 || len(st.states) != 0 {
		t.Fatalf("an alert that never fired must vanish silently: sent=%d states=%v", len(snd.sent), st.states)
	}
}
