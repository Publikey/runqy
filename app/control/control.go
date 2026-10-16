// Package control sends commands to workers over Redis Pub/Sub. Workers (runqy-worker
// v0.4.0+) subscribe to three channels:
//
//	runqy:control              all workers
//	runqy:control:<queue>      workers serving that parent queue
//	runqy:control:<worker_id>  one worker
//
// Delivery is fire-and-forget: a worker that is not subscribed at that moment misses the
// command. Each worker publishes the outcome of its last command in its heartbeat
// ("control" field), which is how the API, CLI and dashboard report results.
package control

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"time"

	queueworker "github.com/Publikey/runqy/queues"
	"github.com/redis/go-redis/v9"
)

// Channel is the broadcast channel; queue and worker channels append ":<name>".
const Channel = "runqy:control"

// Actions understood by workers.
const (
	ActionDrain        = "drain"         // stop taking tasks, let in-flight ones finish
	ActionResume       = "resume"        // undo drain
	ActionCodeUpdate   = "code_update"   // git update + dependencies + process restart
	ActionWorkerUpdate = "worker_update" // install a worker release and restart in place
	ActionConfigReload = "config_reload" // ask the task process to reload its config (@reload)
)

// Actions lists every action, in display order.
var Actions = []string{ActionDrain, ActionResume, ActionCodeUpdate, ActionWorkerUpdate, ActionConfigReload}

// Message is what workers receive.
type Message struct {
	ID      string `json:"id"`
	Action  string `json:"action"`
	Queue   string `json:"queue,omitempty"`   // parent queue targeted (set for queue targets)
	Version string `json:"version,omitempty"` // worker_update
	Timeout string `json:"timeout,omitempty"` // wait for in-flight tasks (Go duration)
	SentAt  int64  `json:"sent_at"`
}

// Request is the API/CLI input.
type Request struct {
	Action  string `json:"action"`
	Target  string `json:"target,omitempty"`  // empty: all workers; a parent queue; or a worker id
	Version string `json:"version,omitempty"` // worker_update only, e.g. "0.4.0"
	Timeout string `json:"timeout,omitempty"`
}

// Result is the API/CLI output.
type Result struct {
	ID        string `json:"id"`
	Action    string `json:"action"`
	Channel   string `json:"channel"`
	Target    string `json:"target"`    // "all", "queue <name>" or "worker <id>"
	Receivers int64  `json:"receivers"` // subscribed connections that got the message
}

// LastCommand is a worker's report of its last command (heartbeat "control" field).
type LastCommand struct {
	ID      string `json:"id"`
	Action  string `json:"action"`
	State   string `json:"state"` // running, done, failed
	Message string `json:"message,omitempty"`
	At      int64  `json:"at"`
}

// ParseLastCommand decodes the heartbeat "control" field (nil when absent or invalid).
func ParseLastCommand(raw string) *LastCommand {
	if raw == "" {
		return nil
	}
	var c LastCommand
	if json.Unmarshal([]byte(raw), &c) != nil {
		return nil
	}
	return &c
}

var versionRe = regexp.MustCompile(`^v?\d+\.\d+\.\d+(-[0-9A-Za-z.-]+)?$`)

// Validate checks a request's action and arguments.
func (r Request) Validate() error {
	known := false
	for _, a := range Actions {
		if r.Action == a {
			known = true
		}
	}
	if !known {
		return fmt.Errorf("unknown action %q (expected one of %s)", r.Action, strings.Join(Actions, ", "))
	}
	if r.Action == ActionWorkerUpdate {
		if !versionRe.MatchString(r.Version) {
			return fmt.Errorf("worker_update needs a release version, e.g. 0.4.0 (got %q)", r.Version)
		}
	} else if r.Version != "" {
		return fmt.Errorf("version only applies to worker_update")
	}
	if r.Timeout != "" {
		if d, err := time.ParseDuration(r.Timeout); err != nil || d <= 0 {
			return fmt.Errorf("invalid timeout %q", r.Timeout)
		}
	}
	return nil
}

// WorkerChannel is the channel of one worker.
func WorkerChannel(workerID string) string { return Channel + ":" + workerID }

// Send resolves the target, then publishes the command. A target is a worker id (a live
// heartbeat), else a parent or sub-queue name (normalised to its parent).
func Send(ctx context.Context, rdb *redis.Client, queues func(ctx context.Context, parent string) (bool, error), req Request) (*Result, error) {
	if err := req.Validate(); err != nil {
		return nil, err
	}
	msg := Message{ID: newID(), Action: req.Action, Version: strings.TrimPrefix(req.Version, "v"), Timeout: req.Timeout, SentAt: time.Now().Unix()}
	res := &Result{ID: msg.ID, Action: req.Action, Channel: Channel, Target: "all"}

	if t := strings.TrimSpace(req.Target); t != "" {
		isWorker, err := rdb.Exists(ctx, "asynq:workers:"+t).Result()
		if err != nil {
			return nil, err
		}
		if isWorker == 1 {
			res.Channel, res.Target = WorkerChannel(t), "worker "+t
		} else {
			parent, _, _ := queueworker.ParseQueueName(t)
			ok, err := queues(ctx, parent)
			if err != nil {
				return nil, err
			}
			if !ok {
				return nil, fmt.Errorf("target %q is neither a live worker nor a queue", t)
			}
			msg.Queue = parent
			res.Channel, res.Target = Channel+":"+parent, "queue "+parent
		}
	}

	n, err := Publish(ctx, rdb, res.Channel, msg)
	if err != nil {
		return nil, err
	}
	res.Receivers = n
	return res, nil
}

// Publish sends a message on a channel and returns the number of receivers.
func Publish(ctx context.Context, rdb *redis.Client, channel string, msg Message) (int64, error) {
	if msg.ID == "" {
		msg.ID = newID()
	}
	if msg.SentAt == 0 {
		msg.SentAt = time.Now().Unix()
	}
	data, err := json.Marshal(msg)
	if err != nil {
		return 0, err
	}
	return rdb.Publish(ctx, channel, data).Result()
}

func newID() string {
	b := make([]byte, 8)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// WorkerFields are the control-related heartbeat fields, embedded in the worker views of
// the API, the dashboard and the health snapshot.
type WorkerFields struct {
	Version        string       `json:"version,omitempty"`
	ControlActions []string     `json:"control_actions,omitempty"` // empty: worker predates the control channel
	Held           []string     `json:"held,omitempty"`            // parent queues not taking new tasks
	LastCommand    *LastCommand `json:"last_command,omitempty"`
}

// ParseWorkerFields reads the control fields of a worker heartbeat hash.
func ParseWorkerFields(data map[string]string) WorkerFields {
	return WorkerFields{
		Version:        data["version"],
		ControlActions: splitList(data["control_actions"]),
		Held:           splitList(data["held"]),
		LastCommand:    ParseLastCommand(data["control"]),
	}
}

// Supports reports whether the worker listens for the action.
func (f WorkerFields) Supports(action string) bool {
	for _, a := range f.ControlActions {
		if a == action {
			return true
		}
	}
	return false
}

func splitList(s string) []string {
	var out []string
	for _, p := range strings.Split(s, ",") {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}
