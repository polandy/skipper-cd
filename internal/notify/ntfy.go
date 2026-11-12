package notify

import (
	"bytes"
	"context"
	"mime"
	"net/http"

	"github.com/polandy/skipper-cd/internal/events"
)

// ntfy publish headers and values; the status travels as a tag and a
// priority, not as emoji in the text.
const (
	ntfyTagOK       = "white_check_mark"
	ntfyTagFailed   = "x"
	ntfyTagUpdate   = "arrow_up"
	ntfyPrioLow     = "low"
	ntfyPrioHigh    = "high"
	ntfyHdrTitle    = "Title"
	ntfyHdrTags     = "Tags"
	ntfyHdrPriority = "Priority"
	ntfyHdrClick    = "Click"
)

// ntfyFormatter publishes the compact deploy message to an ntfy topic URL.
type ntfyFormatter struct {
	url     string
	headers map[string]string
	prefix  string
}

func (f ntfyFormatter) Format(ctx context.Context, ev events.DeployEvent) (*http.Request, error) {
	m := composeDeploy(ev)
	tag, prio := ntfyTagFailed, ntfyPrioHigh
	if m.Success {
		tag, prio = ntfyTagOK, ntfyPrioLow
	}
	return ntfyPost(ctx, f.url, f.headers, ntfyMessage{title: prefixed(f.prefix, m.Title), body: m.Body, tag: tag, priority: prio, click: m.Click})
}

type ntfyMessage struct {
	title, body, tag, priority, click string
}

// ntfyPost builds the publish request. Static headers are applied last so an
// operator can override any of ours (e.g. Priority).
func ntfyPost(ctx context.Context, url string, headers map[string]string, m ntfyMessage) (*http.Request, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader([]byte(m.body)))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "text/plain; charset=utf-8")
	// RFC 2047 keeps a non-ASCII title (→) intact through any proxy; ntfy decodes it.
	req.Header.Set(ntfyHdrTitle, mime.QEncoding.Encode("utf-8", m.title))
	req.Header.Set(ntfyHdrTags, m.tag)
	req.Header.Set(ntfyHdrPriority, m.priority)
	if m.click != "" {
		req.Header.Set(ntfyHdrClick, m.click)
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	return req, nil
}

// prefixed labels a title with the sending host, "[<prefix>] <title>".
func prefixed(prefix, title string) string {
	if prefix == "" {
		return title
	}
	return "[" + prefix + "] " + title
}
