package notify

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/prometheus/client_golang/prometheus"
)

func TestDeliverOne_SendFailureLogsHostNotTokenBearingURL(t *testing.T) {
	const token = "123456:SECRET-TOKEN"
	target := "https://api.telegram.org/bot" + token + "/sendMessage?token=" + token
	var buf bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, nil)))
	t.Cleanup(func() { slog.SetDefault(prev) })

	doer := &fakeDoer{err: &url.Error{Op: "Post", URL: target, Err: errors.New("connection refused")}}
	sent := prometheus.NewCounterVec(prometheus.CounterOpts{Name: "test_sent_total"}, []string{"format", "result"})
	deliverOne(context.Background(), doer, failTimeout,
		func(ctx context.Context) (*http.Request, error) {
			return http.NewRequestWithContext(ctx, http.MethodPost, target, nil)
		}, sent, "telegram", "notification")

	logged := buf.String()
	if !strings.Contains(logged, "send failed") || !strings.Contains(logged, "connection refused") {
		t.Fatalf("send failure not logged with its cause: %q", logged)
	}
	if !strings.Contains(logged, "api.telegram.org") {
		t.Errorf("log should name the host: %q", logged)
	}
	if strings.Contains(logged, "SECRET-TOKEN") {
		t.Errorf("log leaks the URL token: %q", logged)
	}
}
