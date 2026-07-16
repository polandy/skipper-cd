package notify

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/polandy/skipper-cd/internal/config"
	"github.com/polandy/skipper-cd/internal/events"
)

// formatterFor builds the Formatter for a validated config target.
func formatterFor(t config.NotificationTarget) (Formatter, error) {
	switch t.Format {
	case config.NotifyFormatSignal:
		return signalFormatter{base: t.URL, number: t.Number, recipients: t.Recipients, prefix: t.Prefix}, nil
	case config.NotifyFormatGeneric:
		return genericFormatter{url: t.URL, headers: t.Headers}, nil
	default:
		return nil, fmt.Errorf("unknown format %q", t.Format)
	}
}

type signalFormatter struct {
	base       string
	number     string
	recipients []string
	prefix     string
}

func (f signalFormatter) Format(ctx context.Context, ev events.DeployEvent) (*http.Request, error) {
	url := strings.TrimRight(f.base, "/") + "/v2/send"
	body := map[string]any{
		"message":    renderMessage(f.prefix, ev),
		"number":     f.number,
		"recipients": f.recipients,
	}
	return jsonPost(ctx, url, body, nil)
}

type genericFormatter struct {
	url     string
	headers map[string]string
}

func (f genericFormatter) Format(ctx context.Context, ev events.DeployEvent) (*http.Request, error) {
	// Diffs can be large; the notification carries the event metadata only.
	return jsonPost(ctx, f.url, ev.SSEPayload(), f.headers)
}

func jsonPost(ctx context.Context, url string, payload any, headers map[string]string) (*http.Request, error) {
	b, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(b))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	return req, nil
}

// renderMessage renders the human-readable one-liner used by the signal
// formatter's message body. A non-empty prefix is prepended as "[<prefix>] "
// to label the sending host/instance.
func renderMessage(prefix string, ev events.DeployEvent) string {
	if prefix != "" {
		return "[" + prefix + "] " + renderMessage("", ev)
	}
	dur := (time.Duration(ev.DurationMs) * time.Millisecond).Round(time.Millisecond)
	switch ev.Status {
	case events.StatusSuccess:
		return fmt.Sprintf("✅ deploy succeeded: %s (%s)", ev.Stack, dur)
	case events.StatusFailed:
		return fmt.Sprintf("❌ deploy failed: %s (%s) — %s", ev.Stack, dur, ev.Error)
	case events.StatusRolledBack:
		return fmt.Sprintf("↩️ deploy rolled back: %s (%s) — %s", ev.Stack, dur, ev.Error)
	case events.StatusRolledBackUnhealthy:
		return fmt.Sprintf("🚨 deploy rolled back but still unhealthy: %s (%s) — %s", ev.Stack, dur, ev.Error)
	case events.StatusHealExhausted:
		return fmt.Sprintf("🚨 self-heal gave up: %s is still unhealthy after repeated redeploys — %s", ev.Stack, ev.Error)
	default:
		return fmt.Sprintf("deploy %s: %s", ev.Status, ev.Stack)
	}
}
