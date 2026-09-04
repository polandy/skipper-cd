package notify

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/polandy/skipper-cd/internal/config"
	"github.com/polandy/skipper-cd/internal/health"
	"github.com/polandy/skipper-cd/internal/healthwatch"
	"github.com/polandy/skipper-cd/internal/metrics"
)

// HealthFormatter builds the provider-specific HTTP request for a health
// alert — the health counterpart of Formatter.
type HealthFormatter interface {
	Format(ctx context.Context, a healthwatch.Alert) (*http.Request, error)
}

type healthTarget struct {
	format    string // config format name, used as a metric label
	formatter HealthFormatter
}

// HealthAlerter fans health alerts out to its configured targets. It shares
// this package's transport stance (fire-and-forget bounded queue, per-request
// timeout, Doer) but is fully separate from the deploy-event Notifier
// (ADR-0031). It satisfies healthwatch.Alerter.
type HealthAlerter struct {
	*transport[healthwatch.Alert]
	targets []healthTarget
}

// NewHealthAlerter builds a HealthAlerter from health_watch targets. A nil
// doer uses http.DefaultClient; an empty target list yields a disabled
// alerter whose Fire is a no-op.
func NewHealthAlerter(cfgTargets []config.NotificationTarget, doer Doer, timeout time.Duration) (*HealthAlerter, error) {
	targets := make([]healthTarget, 0, len(cfgTargets))
	for i, ct := range cfgTargets {
		f, err := healthFormatterFor(ct)
		if err != nil {
			return nil, fmt.Errorf("health_watch.targets[%d]: %w", i, err)
		}
		targets = append(targets, healthTarget{format: ct.Format, formatter: f})
	}
	return &HealthAlerter{
		transport: newTransport[healthwatch.Alert](doer, timeout, "health alert dropped: buffer full"),
		targets:   targets,
	}, nil
}

// Enabled reports whether any target is configured.
func (a *HealthAlerter) Enabled() bool { return a != nil && len(a.targets) > 0 }

// Fire enqueues a health alert for asynchronous delivery. It never blocks: a
// full buffer drops the alert (the transition is still journaled and persisted
// by the watcher).
func (a *HealthAlerter) Fire(al healthwatch.Alert) {
	if !a.Enabled() {
		return
	}
	a.push(al, "stack", al.Stack, "service", al.Service)
}

// Run delivers queued alerts until ctx is cancelled, then drains what is left.
func (a *HealthAlerter) Run(ctx context.Context) { a.run(ctx, a.handle) }

// handle delivers one alert to every target. A failing target is logged and
// skipped; it never aborts the others.
func (a *HealthAlerter) handle(ctx context.Context, al healthwatch.Alert) {
	for _, t := range a.targets {
		a.deliver(ctx, t, al)
	}
}

func (a *HealthAlerter) deliver(ctx context.Context, t healthTarget, al healthwatch.Alert) {
	deliverOne(ctx, a.doer, a.timeout,
		func(ctx context.Context) (*http.Request, error) { return t.formatter.Format(ctx, al) },
		metrics.HealthAlertsSent, t.format, "health alert", "stack", al.Stack, "service", al.Service)
}

// healthFormatterFor builds the HealthFormatter for a validated health_watch
// target.
func healthFormatterFor(t config.NotificationTarget) (HealthFormatter, error) {
	switch t.Format {
	case config.NotifyFormatSignal:
		return signalHealthFormatter{base: t.URL, number: t.Number, recipients: t.Recipients, prefix: t.Prefix}, nil
	case config.NotifyFormatGeneric:
		return genericHealthFormatter{url: t.URL, headers: t.Headers}, nil
	default:
		return nil, fmt.Errorf("unknown format %q", t.Format)
	}
}

type signalHealthFormatter struct {
	base       string
	number     string
	recipients []string
	prefix     string
}

func (f signalHealthFormatter) Format(ctx context.Context, a healthwatch.Alert) (*http.Request, error) {
	url := strings.TrimRight(f.base, "/") + "/v2/send"
	body := map[string]any{
		"message":    renderHealthMessage(f.prefix, a),
		"number":     f.number,
		"recipients": f.recipients,
	}
	return jsonPost(ctx, url, body, nil)
}

type genericHealthFormatter struct {
	url     string
	headers map[string]string
}

func (f genericHealthFormatter) Format(ctx context.Context, a healthwatch.Alert) (*http.Request, error) {
	// "type":"health" lets a receiver shared with deploy notifications tell
	// the two payload shapes apart.
	payload := map[string]any{
		"type":              "health",
		"stack":             a.Stack,
		"service":           a.Service,
		"from":              a.From,
		"to":                a.To,
		"since":             a.Since,
		"prev_duration_ms":  a.PrevDuration.Milliseconds(),
		"commit":            a.Commit,
		"deploy_correlated": a.DeployCorrelated,
	}
	return jsonPost(ctx, f.url, payload, f.headers)
}

// renderHealthMessage renders the human-readable one-liner for the signal
// format. A non-empty prefix is prepended as "[<prefix>] " to label the
// sending host, matching the deploy notifications.
func renderHealthMessage(prefix string, a healthwatch.Alert) string {
	if prefix != "" {
		return "[" + prefix + "] " + renderHealthMessage("", a)
	}
	prev := a.PrevDuration.Round(time.Second).String()
	if a.To == health.Unhealthy {
		msg := fmt.Sprintf("🚨 stack health: %s/%s %s → %s (was %s %s)",
			a.Stack, a.Service, a.From, a.To, a.From, prev)
		if a.DeployCorrelated && a.Commit != "" {
			msg += " — after deploy of " + shortCommit(a.Commit)
		}
		return msg
	}
	return fmt.Sprintf("✅ stack health recovered: %s/%s after %s", a.Stack, a.Service, prev)
}

// shortCommit renders a SHA at git's customary 7 characters.
func shortCommit(sha string) string {
	if len(sha) > 7 {
		return sha[:7]
	}
	return sha
}
