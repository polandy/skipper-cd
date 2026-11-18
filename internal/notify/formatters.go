package notify

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

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
	case config.NotifyFormatNtfy:
		return ntfyFormatter{url: t.URL, headers: t.Headers, prefix: t.Prefix}, nil
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

// renderMessage renders the compact text of the signal formatter: the title,
// then the body on the next line. A non-empty prefix is prepended as
// "[<prefix>] " to label the sending host/instance.
func renderMessage(prefix string, ev events.DeployEvent) string {
	m := composeDeploy(ev)
	msg := m.Title
	if prefix != "" {
		msg = "[" + prefix + "] " + msg
	}
	if m.Body != "" {
		msg += "\n" + m.Body
	}
	return msg
}

// imageTag returns an image reference's tag, or "" when it carries none
// ("nginx:1.25@ab12" → "1.25", "nginx@ab12" → "").
func imageTag(ref string) string {
	if at := strings.IndexByte(ref, '@'); at != -1 {
		ref = ref[:at]
	}
	colon := strings.LastIndexByte(ref, ':')
	if colon <= strings.LastIndexByte(ref, '/') {
		return ""
	}
	return ref[colon+1:]
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
