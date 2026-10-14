package alerts

import (
	"context"
	"fmt"
	"log"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/Publikey/runqy/health"
	"github.com/Publikey/runqy/leader"
	queueworker "github.com/Publikey/runqy/queues"
	"github.com/redis/go-redis/v9"
)

// EvalInterval is how often alert rules are evaluated.
const EvalInterval = 30 * time.Second

// minEventInterval throttles one-off events (per queue and rule) when repeat_interval
// is 0, so a provider that keeps failing does not send an email every poll.
const minEventInterval = time.Hour

type healthCollector interface {
	Collect(ctx context.Context, now time.Time) ([]health.QueueHealth, error)
}

type settingsSource interface {
	Get(ctx context.Context) (*queueworker.AlertSettings, error)
}

// Engine evaluates alert rules and sends notifications. Only the server holding the
// leader lock evaluates; state is kept in Redis.
type Engine struct {
	collector healthCollector
	settings  settingsSource
	state     stateStore
	newSender func(ctx context.Context, s *queueworker.AlertSettings) (Sender, error)
	lock      *leader.Lock
	publicURL string

	stopOnce sync.Once
	stopCh   chan struct{}
	doneCh   chan struct{}
}

// NewEngine creates an engine. vault may be nil when vaults are disabled.
func NewEngine(collector *health.Collector, settings *SettingsStore, rdb *redis.Client, vault VaultReader, publicURL string) *Engine {
	return &Engine{
		collector: collector,
		settings:  settings,
		state:     redisState{rdb: rdb},
		newSender: func(ctx context.Context, s *queueworker.AlertSettings) (Sender, error) {
			return NewSender(ctx, s, vault)
		},
		lock:      leader.New(rdb, "runqy:alerts:leader", 2*EvalInterval),
		publicURL: strings.TrimRight(publicURL, "/"),
		stopCh:    make(chan struct{}),
		doneCh:    make(chan struct{}),
	}
}

// Start launches the evaluation loop.
func (e *Engine) Start() { go e.run() }

// Stop ends the loop and waits for it. Safe to call more than once.
func (e *Engine) Stop() {
	e.stopOnce.Do(func() { close(e.stopCh) })
	<-e.doneCh
}

func (e *Engine) run() {
	defer close(e.doneCh)
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		e.lock.Release(ctx)
	}()
	ticker := time.NewTicker(EvalInterval)
	defer ticker.Stop()
	for {
		select {
		case <-e.stopCh:
			return
		case <-ticker.C:
			ctx, cancel := context.WithTimeout(context.Background(), EvalInterval-5*time.Second)
			if e.lock.Acquire(ctx) {
				if err := e.Evaluate(ctx, time.Now()); err != nil {
					log.Printf("[ALERTS] evaluation failed: %v", err)
				}
			}
			cancel()
		}
	}
}

// condition is the current truth of one (queue, rule).
type condition struct {
	queue   string
	rule    queueworker.AlertRule
	active  bool
	message string
}

// change is the outcome for one alert key. fallback is what to persist instead when the
// notification could not be sent, so the next evaluation retries it.
type change struct {
	key      string
	next     *alertState // nil = remove
	fallback *alertState // nil = remove
	note     *HistoryEntry
}

func alertKey(queue, rule string) string { return queue + "|" + rule }

// Evaluate runs one evaluation: compute conditions, apply state transitions, send one
// digest email for everything that changed.
func (e *Engine) Evaluate(ctx context.Context, now time.Time) error {
	settings, err := e.settings.Get(ctx)
	if err != nil {
		e.recordError(ctx, now, err)
		return err
	}
	if settings == nil || !settings.Enabled {
		return nil
	}

	queues, err := e.collector.Collect(ctx, now)
	if err != nil {
		err = fmt.Errorf("collect queue health: %w", err)
		e.recordError(ctx, now, err)
		return err
	}
	prev, err := e.state.load(ctx)
	if err != nil {
		return fmt.Errorf("load alert state: %w", err)
	}
	events, err := e.state.drainEvents(ctx)
	if err != nil {
		log.Printf("[ALERTS] cannot read events: %v", err)
	}

	conds := evaluateConditions(settings, queues)
	changes := planChanges(settings, conds, events, queues, prev, now)

	var notes []HistoryEntry
	for _, c := range changes {
		if c.note != nil {
			notes = append(notes, *c.note)
		}
	}

	sendErr := error(nil)
	if len(notes) > 0 {
		sendErr = e.send(ctx, settings, notes, now)
	}

	upsert := map[string]*alertState{}
	var remove []string
	for _, c := range changes {
		st := c.next
		if sendErr != nil {
			st = c.fallback
		}
		if st == nil {
			if _, existed := prev[c.key]; existed {
				remove = append(remove, c.key)
			}
		} else {
			upsert[c.key] = st
		}
	}
	if err := e.state.save(ctx, upsert, remove); err != nil {
		return fmt.Errorf("save alert state: %w", err)
	}

	status, _ := e.state.getStatus(ctx)
	status.LastEval = now
	if sendErr != nil {
		status.LastError = sendErr.Error()
		status.LastErrorAt = now
		_ = e.state.setStatus(ctx, status)
		return fmt.Errorf("send notification: %w", sendErr)
	}
	if len(notes) > 0 {
		status.LastSent = now
		status.LastError = ""
		if err := e.state.appendHistory(ctx, notes); err != nil {
			log.Printf("[ALERTS] cannot append history: %v", err)
		}
		log.Printf("[ALERTS] sent %d notification(s)", len(notes))
	}
	_ = e.state.setStatus(ctx, status)
	return nil
}

func (e *Engine) send(ctx context.Context, settings *queueworker.AlertSettings, notes []HistoryEntry, now time.Time) error {
	sender, err := e.newSender(ctx, settings)
	if err != nil {
		return err
	}
	return sender.Send(ctx, renderDigest(settings, notes, e.publicURL, now))
}

func (e *Engine) recordError(ctx context.Context, now time.Time, err error) {
	status, _ := e.state.getStatus(ctx)
	status.LastEval = now
	status.LastError = err.Error()
	status.LastErrorAt = now
	_ = e.state.setStatus(ctx, status)
}

// evaluateConditions computes every (queue, rule) condition. Paused queues are skipped:
// a growing backlog is expected there.
func evaluateConditions(settings *queueworker.AlertSettings, queues []health.QueueHealth) map[string]condition {
	out := map[string]condition{}
	for _, q := range queues {
		if q.Paused {
			continue
		}
		var override *queueworker.QueueAlertsConfig
		if q.Deployment != nil {
			override = q.Deployment.Alerts
		}
		for _, rule := range queueworker.EffectiveAlertRules(settings.Rules, override) {
			active, msg, ok := checkRule(rule, q)
			if !ok {
				continue
			}
			out[alertKey(q.Name, rule.Type)] = condition{queue: q.Name, rule: rule, active: active, message: msg}
		}
	}
	return out
}

// checkRule evaluates one state rule; ok is false for event-only rules.
func checkRule(rule queueworker.AlertRule, q health.QueueHealth) (active bool, msg string, ok bool) {
	switch rule.Type {
	case queueworker.AlertNoWorkers:
		active = q.Workers == 0 && (q.Pending > 0 || rule.WhenIdle)
		return active, fmt.Sprintf("no live worker is serving the queue (%d job(s) pending)", q.Pending), true
	case queueworker.AlertLatency:
		max := rule.MaxLatencyDuration()
		active = q.Pending > 0 && max > 0 && q.Latency > max
		return active, fmt.Sprintf("oldest pending job has waited %s (threshold %s)", roundDuration(q.Latency), max), true
	case queueworker.AlertPending:
		active = rule.Threshold > 0 && q.Pending > rule.Threshold
		return active, fmt.Sprintf("%d jobs pending (threshold %d)", q.Pending, rule.Threshold), true
	case queueworker.AlertWorkerUnhealthy:
		active = len(q.UnhealthyWorkers) > 0
		return active, fmt.Sprintf("%d unhealthy worker(s): %s", len(q.UnhealthyWorkers), strings.Join(q.UnhealthyWorkers, ", ")), true
	}
	return false, "", false
}

// planChanges turns conditions and events into state transitions and notifications.
func planChanges(settings *queueworker.AlertSettings, conds map[string]condition, events []Event, queues []health.QueueHealth, prev map[string]*alertState, now time.Time) []change {
	repeat := settings.RepeatIntervalOrDefault()
	sendResolved := settings.SendResolvedOrDefault()
	var out []change

	keys := make([]string, 0, len(conds))
	for k := range conds {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	for _, key := range keys {
		c := conds[key]
		old := prev[key]
		if !c.active {
			if old == nil {
				continue
			}
			ch := change{key: key}
			if old.Firing && sendResolved {
				ch.note = &HistoryEntry{Time: now, Queue: c.queue, Rule: c.rule.Type, Status: StatusResolved,
					Message: fmt.Sprintf("resolved after %s", roundDuration(now.Sub(old.FiredAt)))}
				ch.fallback = old // retry the resolved notification next time
			}
			out = append(out, ch)
			continue
		}

		st := &alertState{Queue: c.queue, Rule: c.rule.Type, Since: now}
		if old != nil {
			cp := *old
			st = &cp
		}
		st.Message = c.message
		fallback := *st // condition tracking only, no notification bookkeeping

		var note *HistoryEntry
		switch {
		case !st.Firing && now.Sub(st.Since) >= c.rule.ForDuration():
			st.Firing, st.FiredAt, st.LastNotified = true, now, now
			note = &HistoryEntry{Time: now, Queue: c.queue, Rule: c.rule.Type, Status: StatusFiring, Message: c.message}
		case st.Firing && repeat > 0 && now.Sub(st.LastNotified) >= repeat:
			st.LastNotified = now
			note = &HistoryEntry{Time: now, Queue: c.queue, Rule: c.rule.Type, Status: StatusRepeat, Message: c.message}
		}
		out = append(out, change{key: key, next: st, fallback: &fallback, note: note})
	}

	// One-off events, throttled per (queue, rule).
	eventThrottle := repeat
	if eventThrottle <= 0 {
		eventThrottle = minEventInterval
	}
	allowed := map[string]bool{}
	for _, q := range queues {
		var override *queueworker.QueueAlertsConfig
		if q.Deployment != nil {
			override = q.Deployment.Alerts
		}
		for _, r := range queueworker.EffectiveAlertRules(settings.Rules, override) {
			allowed[alertKey(q.Name, r.Type)] = true
		}
	}
	grouped := map[string][]Event{}
	var eventKeys []string
	for _, ev := range events {
		key := alertKey(ev.Queue, ev.Rule)
		if !allowed[key] {
			continue
		}
		if _, ok := grouped[key]; !ok {
			eventKeys = append(eventKeys, key)
		}
		grouped[key] = append(grouped[key], ev)
	}
	handled := map[string]bool{}
	for _, key := range eventKeys {
		evs := grouped[key]
		handled[key] = true
		old := prev[key]
		if old != nil && now.Sub(old.LastNotified) < eventThrottle {
			continue // still throttled; keep the existing state
		}
		msg := evs[len(evs)-1].Message
		if len(evs) > 1 {
			msg = fmt.Sprintf("%s (%d occurrences)", msg, len(evs))
		}
		st := &alertState{Queue: evs[0].Queue, Rule: evs[0].Rule, Since: evs[0].Time, LastNotified: now, Message: msg}
		out = append(out, change{key: key, next: st, fallback: old,
			note: &HistoryEntry{Time: now, Queue: st.Queue, Rule: st.Rule, Status: StatusEvent, Message: msg}})
	}

	// States whose rule no longer applies (queue removed, muted or paused, rule removed):
	// drop silently. Event throttle entries are kept until their window has passed.
	for key, old := range prev {
		if _, ok := conds[key]; ok || handled[key] {
			continue
		}
		if old.Rule == queueworker.AlertAutoscaleFailure && now.Sub(old.LastNotified) < eventThrottle {
			continue
		}
		out = append(out, change{key: key})
	}
	return out
}

func roundDuration(d time.Duration) time.Duration {
	switch {
	case d >= time.Hour:
		return d.Round(time.Minute)
	case d >= time.Minute:
		return d.Round(time.Second)
	default:
		return d.Round(time.Second)
	}
}

// ActiveAlert is a condition currently true for a queue (firing, or pending its `for`).
type ActiveAlert struct {
	Queue   string    `json:"queue"`
	Rule    string    `json:"rule"`
	Firing  bool      `json:"firing"`
	Since   time.Time `json:"since"`
	FiredAt time.Time `json:"fired_at,omitzero"`
	Message string    `json:"message"`
}

// Active returns the current alert conditions, firing first.
func Active(ctx context.Context, rdb *redis.Client) ([]ActiveAlert, error) {
	states, err := redisState{rdb: rdb}.load(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]ActiveAlert, 0, len(states))
	for _, st := range states {
		if st.Rule == queueworker.AlertAutoscaleFailure {
			continue // event throttle bookkeeping, not a condition
		}
		out = append(out, ActiveAlert{Queue: st.Queue, Rule: st.Rule, Firing: st.Firing, Since: st.Since, FiredAt: st.FiredAt, Message: st.Message})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Firing != out[j].Firing {
			return out[i].Firing
		}
		if out[i].Queue != out[j].Queue {
			return out[i].Queue < out[j].Queue
		}
		return out[i].Rule < out[j].Rule
	})
	return out, nil
}

// GetStatus returns the evaluator status.
func GetStatus(ctx context.Context, rdb *redis.Client) (Status, error) {
	return redisState{rdb: rdb}.getStatus(ctx)
}

// SendTest sends a test email with the given settings.
func SendTest(ctx context.Context, settings *queueworker.AlertSettings, vault VaultReader, publicURL string) error {
	if len(settings.Recipients) == 0 || settings.From == "" {
		return fmt.Errorf("from and at least one recipient are required")
	}
	sender, err := NewSender(ctx, settings, vault)
	if err != nil {
		return err
	}
	return sender.Send(ctx, renderTest(settings, strings.TrimRight(publicURL, "/"), time.Now()))
}
