package alerts

import (
	"fmt"
	"html"
	"net/url"
	"strings"
	"time"

	queueworker "github.com/Publikey/runqy/queues"
)

var ruleTitles = map[string]string{
	queueworker.AlertNoWorkers:        "No workers",
	queueworker.AlertLatency:          "High latency",
	queueworker.AlertPending:          "Backlog",
	queueworker.AlertWorkerUnhealthy:  "Unhealthy worker",
	queueworker.AlertAutoscaleFailure: "Autoscaling failure",
}

var statusLabels = map[string]string{
	StatusFiring:   "FIRING",
	StatusRepeat:   "STILL FIRING",
	StatusResolved: "RESOLVED",
	StatusEvent:    "EVENT",
}

var statusColors = map[string]string{
	StatusFiring:   "#c62828",
	StatusRepeat:   "#c62828",
	StatusResolved: "#2e7d32",
	StatusEvent:    "#ef6c00",
}

func ruleTitle(rule string) string {
	if t, ok := ruleTitles[rule]; ok {
		return t
	}
	return rule
}

func queueURL(publicURL, queue string) string {
	if publicURL == "" {
		return ""
	}
	return publicURL + "/monitoring/queues/" + url.PathEscape(queue)
}

// renderDigest builds one email for every notification of an evaluation.
func renderDigest(settings *queueworker.AlertSettings, notes []HistoryEntry, publicURL string, now time.Time) Email {
	counts := map[string]int{}
	for _, n := range notes {
		counts[n.Status]++
	}

	var subject string
	if len(notes) == 1 {
		n := notes[0]
		subject = fmt.Sprintf("[runqy] %s %s: %s", statusLabels[n.Status], n.Queue, ruleTitle(n.Rule))
	} else {
		var parts []string
		for _, s := range []string{StatusFiring, StatusRepeat, StatusEvent, StatusResolved} {
			if counts[s] > 0 {
				parts = append(parts, fmt.Sprintf("%d %s", counts[s], strings.ToLower(statusLabels[s])))
			}
		}
		subject = "[runqy] " + strings.Join(parts, ", ") + " — " + queueList(notes, 3)
	}

	var text strings.Builder
	var rows strings.Builder
	for _, n := range notes {
		fmt.Fprintf(&text, "[%s] %s: %s\n  %s\n", statusLabels[n.Status], n.Queue, ruleTitle(n.Rule), n.Message)
		link := queueURL(publicURL, n.Queue)
		if link != "" {
			fmt.Fprintf(&text, "  %s\n", link)
		}
		text.WriteString("\n")

		queueCell := html.EscapeString(n.Queue)
		if link != "" {
			queueCell = fmt.Sprintf(`<a href="%s" style="color:#1565c0">%s</a>`, html.EscapeString(link), queueCell)
		}
		fmt.Fprintf(&rows, `<tr>
<td style="padding:8px 12px;border-bottom:1px solid #eee"><span style="color:#fff;background:%s;border-radius:4px;padding:2px 6px;font-size:12px;font-weight:600">%s</span></td>
<td style="padding:8px 12px;border-bottom:1px solid #eee">%s</td>
<td style="padding:8px 12px;border-bottom:1px solid #eee">%s</td>
<td style="padding:8px 12px;border-bottom:1px solid #eee;color:#555">%s</td>
</tr>`, statusColors[n.Status], statusLabels[n.Status], queueCell, html.EscapeString(ruleTitle(n.Rule)), html.EscapeString(n.Message))
	}
	footer := fmt.Sprintf("Sent by runqy at %s.", now.UTC().Format("2006-01-02 15:04 UTC"))
	if repeat := settings.RepeatIntervalOrDefault(); repeat > 0 {
		footer += fmt.Sprintf(" Alerts still firing are re-sent every %s.", repeat)
	}
	text.WriteString(footer + "\n")

	htmlBody := fmt.Sprintf(`<!doctype html><html><body style="font-family:-apple-system,Segoe UI,Roboto,sans-serif;color:#222;margin:0;padding:24px">
<h2 style="margin:0 0 16px;font-size:18px">%s</h2>
<table style="border-collapse:collapse;width:100%%;font-size:14px">
<tr style="text-align:left;color:#777;font-size:12px"><th style="padding:4px 12px">Status</th><th style="padding:4px 12px">Queue</th><th style="padding:4px 12px">Alert</th><th style="padding:4px 12px">Details</th></tr>
%s
</table>
<p style="color:#888;font-size:12px;margin-top:24px">%s</p>
</body></html>`, html.EscapeString(strings.TrimPrefix(subject, "[runqy] ")), rows.String(), html.EscapeString(footer))

	return Email{From: settings.From, To: settings.Recipients, Subject: subject, Text: text.String(), HTML: htmlBody}
}

func renderTest(settings *queueworker.AlertSettings, publicURL string, now time.Time) Email {
	body := fmt.Sprintf("This is a test notification from runqy (%s, sent %s).\nAlert emails will reach these recipients: %s.",
		settings.Provider, now.UTC().Format("2006-01-02 15:04 UTC"), strings.Join(settings.Recipients, ", "))
	if publicURL != "" {
		body += "\nDashboard: " + publicURL + "/monitoring"
	}
	return Email{
		From:    settings.From,
		To:      settings.Recipients,
		Subject: "[runqy] Test notification",
		Text:    body + "\n",
		HTML: `<!doctype html><html><body style="font-family:-apple-system,Segoe UI,Roboto,sans-serif;padding:24px"><p>` +
			strings.ReplaceAll(html.EscapeString(body), "\n", "<br>") + `</p></body></html>`,
	}
}

// queueList names the distinct queues of notes, at most max of them ("a, b +2").
func queueList(notes []HistoryEntry, max int) string {
	var names []string
	seen := map[string]bool{}
	for _, n := range notes {
		if !seen[n.Queue] {
			seen[n.Queue] = true
			names = append(names, n.Queue)
		}
	}
	if len(names) <= max {
		return strings.Join(names, ", ")
	}
	return fmt.Sprintf("%s +%d", strings.Join(names[:max], ", "), len(names)-max)
}
