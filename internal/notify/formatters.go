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
	services := renderImageChanges(ev.ImageChanges)
	switch ev.Status {
	case events.StatusSuccess:
		// The health gate is reported only here: on a success it is the one thing
		// the outcome alone does not say (applied vs. verified healthy), while a
		// failure message already names what went wrong.
		return fmt.Sprintf("✅ deploy succeeded: %s (%s)", ev.Stack, dur) + services + renderHealthGate(ev.HealthGated)
	case events.StatusFailed:
		return fmt.Sprintf("❌ deploy failed: %s (%s) — %s", ev.Stack, dur, ev.Error) + services
	case events.StatusRolledBack:
		return fmt.Sprintf("↩️ deploy rolled back: %s (%s) — %s", ev.Stack, dur, ev.Error) + services
	case events.StatusRolledBackUnhealthy:
		return fmt.Sprintf("🚨 deploy rolled back but still unhealthy: %s (%s) — %s", ev.Stack, dur, ev.Error) + services
	case events.StatusHealExhausted:
		return fmt.Sprintf("🚨 self-heal gave up: %s is still unhealthy after repeated redeploys — %s", ev.Stack, ev.Error)
	default:
		return fmt.Sprintf("deploy %s: %s", ev.Status, ev.Stack)
	}
}

// msgIndent starts every detail line appended below a deploy message's headline.
const msgIndent = "\n  "

// renderImageChanges renders the per-service image changes as indented lines
// appended to a deploy message, or "" when there are none. Each line reads
// "<indent>• <service>: <old> → <new>"; a service with no recorded previous
// image (first deploy) shows just the new ref, a removed service
// "<old> (removed)".
func renderImageChanges(changes []events.ServiceImageChange) string {
	if len(changes) == 0 {
		return ""
	}
	var b strings.Builder
	for _, c := range changes {
		b.WriteString(msgIndent + "• ")
		b.WriteString(c.Service)
		b.WriteString(": ")
		switch {
		case c.New == "":
			b.WriteString(c.Old + " (removed)")
		case c.Old == "":
			b.WriteString(c.New)
		default:
			oldRef, newRef := versionTokens(c.Old, c.New)
			b.WriteString(oldRef)
			b.WriteString(" → ")
			b.WriteString(newRef)
		}
	}
	return b.String()
}

// renderHealthGate renders the gate line appended to a successful deploy
// message, or "" when the deploy ran without an effective deploy_health_check.
// An absent line reads as "not gated" — the message never claims a verification
// that did not happen.
func renderHealthGate(gated bool) string {
	if !gated {
		return ""
	}
	return msgIndent + "✓ health gate passed"
}

// versionTokens reduces an old → new image reference pair to the tokens that
// actually differ. Both sides normally share a repository — the service name
// beside them already says which image it is — so it is dropped and only the
// version part is shown ("nextcloud:30@ab12…" → "30@ab12…"). When the
// repositories differ, the service switched images entirely: both references
// are kept in full, because that is the change.
func versionTokens(oldRef, newRef string) (string, string) {
	if imageRepository(oldRef) != imageRepository(newRef) {
		return oldRef, newRef
	}
	return dropRepository(oldRef), dropRepository(newRef)
}

// imageRepository returns the repository part of an image reference: registry
// and path kept, tag and digest/image id dropped
// ("ghcr.io/acme/api:1.5@ab12" → "ghcr.io/acme/api").
func imageRepository(ref string) string {
	if at := strings.IndexByte(ref, '@'); at != -1 {
		ref = ref[:at]
	}
	if colon := strings.LastIndexByte(ref, ':'); colon > strings.LastIndexByte(ref, '/') {
		ref = ref[:colon]
	}
	return ref
}

// dropRepository strips the repository prefix from an image reference, leaving
// the tokens that identify the version ("nginx:1.25@ab12" → "1.25@ab12"). A
// bare repository with no version tokens is returned unchanged rather than
// reduced to nothing.
func dropRepository(ref string) string {
	rest := strings.TrimPrefix(strings.TrimPrefix(ref, imageRepository(ref)), ":")
	if rest == "" {
		return ref
	}
	return rest
}
