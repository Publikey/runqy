package provider

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// Vast.ai serves logs asynchronously: PUT /instances/request_logs/{id}/ returns a
// pre-signed result_url, and the file appears there a moment later (the official CLI
// polls it 30 times, 0.3 s apart).
const (
	vastLogsPollAttempts = 30
	vastLogsPollInterval = 500 * time.Millisecond
	vastLogsMaxBytes     = 1 << 20
)

var blankLines = regexp.MustCompile(`\n\s*\n`)

// Logs returns the end of the instance's container logs. When the container produced
// nothing (it never started: image pull failure, GPU not attachable...), the host
// daemon's own logs are returned instead, as they then hold the actual error.
func (p *vastAIProvider) Logs(ctx context.Context, instanceID string, tail int) (string, error) {
	container, cerr := p.requestLogs(ctx, instanceID, tail, false)
	if cerr == nil && strings.TrimSpace(container) != "" {
		return container, nil
	}
	daemon, derr := p.requestLogs(ctx, instanceID, tail, true)
	if derr == nil && strings.TrimSpace(daemon) != "" {
		return "[container produced no output; host daemon logs follow]\n" + daemon, nil
	}
	if cerr != nil {
		return "", cerr
	}
	if derr != nil {
		return "", derr
	}
	return "", nil
}

func (p *vastAIProvider) requestLogs(ctx context.Context, instanceID string, tail int, daemon bool) (string, error) {
	body := map[string]string{"tail": strconv.Itoa(tail)}
	if daemon {
		body["daemon_logs"] = "true"
	}
	var resp struct {
		ResultURL string `json:"result_url"`
		Msg       string `json:"msg"`
	}
	if err := p.do(ctx, http.MethodPut, "/instances/request_logs/"+instanceID+"/", body, &resp); err != nil {
		return "", err
	}
	if resp.ResultURL == "" {
		return "", fmt.Errorf("vastai: no logs available for instance %s: %s", instanceID, resp.Msg)
	}

	for attempt := 0; attempt < vastLogsPollAttempts; attempt++ {
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		case <-time.After(vastLogsPollInterval):
		}
		// The result URL is pre-signed: no Authorization header (never send the API key
		// to the storage host).
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, resp.ResultURL, nil)
		if err != nil {
			return "", err
		}
		r, err := p.client.Do(req)
		if err != nil {
			continue
		}
		data, _ := io.ReadAll(io.LimitReader(r.Body, vastLogsMaxBytes))
		r.Body.Close()
		if r.StatusCode == http.StatusOK {
			return blankLines.ReplaceAllString(string(data), "\n"), nil
		}
	}
	return "", fmt.Errorf("vastai: logs for instance %s were not ready in time", instanceID)
}
