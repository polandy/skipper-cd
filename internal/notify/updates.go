package notify

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/polandy/skipper-cd/internal/config"
	"github.com/polandy/skipper-cd/internal/metrics"
	"github.com/polandy/skipper-cd/internal/updatecheck"
)

// UpdateFormatter builds the provider-specific HTTP request for an
// image-update notification — the update-check counterpart of Formatter.
type UpdateFormatter interface {
	Format(ctx context.Context, a updatecheck.Alert) (*http.Request, error)
}

type updateTarget struct {
	format    string // config format name, used as a metric label
	formatter UpdateFormatter
}

// UpdateAlerter fans image-update notifications (ADR-0054) out to the
// configured notifications targets. Like the HealthAlerter it shares this
// package's transport stance (fire-and-forget bounded queue, per-request
// timeout, Doer) but stays separate from the deploy-event Notifier; targets'
// `on:` filters deploy statuses only, so every target receives updates.
// Dedup is the update checker's job — Fire delivers what it is handed.
type UpdateAlerter struct {
	targets []updateTarget
	doer    Doer
	timeout time.Duration
	queue   chan updatecheck.Alert
}

// NewUpdateAlerter builds an UpdateAlerter from notification targets. A nil
// doer uses http.DefaultClient; an empty target list yields a disabled
// alerter whose Fire is a no-op.
func NewUpdateAlerter(cfgTargets []config.NotificationTarget, doer Doer, timeout time.Duration) (*UpdateAlerter, error) {
	targets := make([]updateTarget, 0, len(cfgTargets))
	for i, ct := range cfgTargets {
		f, err := updateFormatterFor(ct)
		if err != nil {
			return nil, fmt.Errorf("notifications[%d]: %w", i, err)
		}
		targets = append(targets, updateTarget{format: ct.Format, formatter: f})
	}
	if doer == nil {
		doer = http.DefaultClient
	}
	if timeout <= 0 {
		timeout = defaultRequestTimeout
	}
	return &UpdateAlerter{
		targets: targets,
		doer:    doer,
		timeout: timeout,
		queue:   make(chan updatecheck.Alert, notifyBufferSize),
	}, nil
}

// Enabled reports whether any target is configured.
func (a *UpdateAlerter) Enabled() bool { return a != nil && len(a.targets) > 0 }

// Fire enqueues an update notification for asynchronous delivery. It never
// blocks: a full buffer drops the alert (the update stays visible in the UI).
func (a *UpdateAlerter) Fire(al updatecheck.Alert) {
	if !a.Enabled() {
		return
	}
	select {
	case a.queue <- al:
	default:
		slog.Warn("update notification dropped: buffer full", "stack", al.Stack, "service", al.Service)
		metrics.NotificationsDropped.Inc()
	}
}

// Run consumes queued alerts until ctx is cancelled, then best-effort drains
// what is buffered within one timeout and returns. Intended to run in its own
// goroutine; the caller must not block shutdown on it (ADR-0014 ethos).
func (a *UpdateAlerter) Run(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			a.drain()
			return
		case al := <-a.queue:
			a.handle(ctx, al)
		}
	}
}

func (a *UpdateAlerter) drain() {
	ctx, cancel := context.WithTimeout(context.Background(), a.timeout)
	defer cancel()
	for {
		select {
		case al := <-a.queue:
			a.handle(ctx, al)
		default:
			return
		}
	}
}

// handle delivers one alert to every target. A failing target is logged and
// skipped; it never aborts the others.
func (a *UpdateAlerter) handle(ctx context.Context, al updatecheck.Alert) {
	for _, t := range a.targets {
		deliverOne(ctx, a.doer, a.timeout,
			func(ctx context.Context) (*http.Request, error) { return t.formatter.Format(ctx, al) },
			metrics.UpdateAlertsSent, t.format, "update notification", "stack", al.Stack, "service", al.Service)
	}
}

// updateFormatterFor builds the UpdateFormatter for a validated notifications
// target.
func updateFormatterFor(t config.NotificationTarget) (UpdateFormatter, error) {
	switch t.Format {
	case config.NotifyFormatSignal:
		return signalUpdateFormatter{base: t.URL, number: t.Number, recipients: t.Recipients, prefix: t.Prefix}, nil
	case config.NotifyFormatGeneric:
		return genericUpdateFormatter{url: t.URL, headers: t.Headers}, nil
	default:
		return nil, fmt.Errorf("unknown format %q", t.Format)
	}
}

type signalUpdateFormatter struct {
	base       string
	number     string
	recipients []string
	prefix     string
}

func (f signalUpdateFormatter) Format(ctx context.Context, a updatecheck.Alert) (*http.Request, error) {
	url := strings.TrimRight(f.base, "/") + "/v2/send"
	body := map[string]any{
		"message":    renderUpdateMessage(f.prefix, a),
		"number":     f.number,
		"recipients": f.recipients,
	}
	return jsonPost(ctx, url, body, nil)
}

type genericUpdateFormatter struct {
	url     string
	headers map[string]string
}

func (f genericUpdateFormatter) Format(ctx context.Context, a updatecheck.Alert) (*http.Request, error) {
	// "type":"update" lets a receiver shared with the other notification kinds
	// tell the payload shapes apart.
	payload := map[string]any{
		"type":    "update",
		"stack":   a.Stack,
		"service": a.Service,
		"running": a.Running,
		"latest":  a.Latest,
		"rebuilt": a.Rebuilt,
	}
	return jsonPost(ctx, f.url, payload, f.headers)
}

// renderUpdateMessage renders the human-readable one-liner for the signal
// format. A non-empty prefix is prepended as "[<prefix>] " to label the
// sending host, matching the other notification kinds.
func renderUpdateMessage(prefix string, a updatecheck.Alert) string {
	if prefix != "" {
		return "[" + prefix + "] " + renderUpdateMessage("", a)
	}
	if a.Latest != "" {
		return fmt.Sprintf("⬆️ image update: %s/%s %s available (running %s)", a.Stack, a.Service, a.Latest, a.Running)
	}
	return fmt.Sprintf("⬆️ image update: %s/%s tag %s was rebuilt upstream", a.Stack, a.Service, a.Running)
}
