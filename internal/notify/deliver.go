package notify

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"time"

	"github.com/prometheus/client_golang/prometheus"
)

// Result labels for the outbound *Sent counters.
const (
	sendResultOK    = "ok"
	sendResultError = "error"
)

// deliverOne runs the outbound pipeline shared by the deploy Notifier and the
// HealthAlerter: build the provider request, send it, drain and close the
// body, and count the outcome — they differ only in event type, metric and
// log labels. sent is counted under formatLabel and the result; kind prefixes
// the log lines ("notification", "health alert") and attrs identify the event
// in them.
func deliverOne(ctx context.Context, doer Doer, timeout time.Duration,
	format func(context.Context) (*http.Request, error),
	sent *prometheus.CounterVec, formatLabel, kind string, attrs ...any) {

	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	fail := func(msg string, kv ...any) {
		slog.Error(kind+" "+msg, append(append([]any{}, attrs...), kv...)...)
		sent.WithLabelValues(formatLabel, sendResultError).Inc()
	}

	req, err := format(ctx)
	if err != nil {
		fail("format failed", errAttrs(err)...)
		return
	}
	resp, err := doer.Do(req)
	if err != nil {
		fail("send failed", errAttrs(err)...)
		return
	}
	defer func() { _ = resp.Body.Close() }()
	_, _ = io.Copy(io.Discard, resp.Body)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		fail("rejected", "status", resp.StatusCode)
		return
	}
	sent.WithLabelValues(formatLabel, sendResultOK).Inc()
}

// errAttrs logs a request error by host and cause only: notification URLs carry
// tokens in their path or query, and logs are served at /api/logs.
func errAttrs(err error) []any {
	var ue *url.Error
	if !errors.As(err, &ue) {
		return []any{"err", err}
	}
	host := ""
	if u, perr := url.Parse(ue.URL); perr == nil {
		host = u.Host
	}
	return []any{"host", host, "err", ue.Err}
}
