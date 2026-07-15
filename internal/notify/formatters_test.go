package notify

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/polandy/skipper-cd/internal/config"
	"github.com/polandy/skipper-cd/internal/events"
)

func mustFormat(t *testing.T, f Formatter, ev events.DeployEvent) *http.Request {
	t.Helper()
	req, err := f.Format(context.Background(), ev)
	if err != nil {
		t.Fatalf("Format: %v", err)
	}
	if req.Method != http.MethodPost {
		t.Errorf("method = %s, want POST", req.Method)
	}
	if ct := req.Header.Get("Content-Type"); ct != "application/json" {
		t.Errorf("Content-Type = %q, want application/json", ct)
	}
	return req
}

func bodyOf(t *testing.T, req *http.Request) map[string]any {
	t.Helper()
	b, err := io.ReadAll(req.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatalf("body is not JSON object: %v (%s)", err, b)
	}
	return m
}

func TestSignalFormatter_V2SendBody(t *testing.T) {
	f := signalFormatter{base: "http://localhost:8020/", number: "+49111", recipients: []string{"+49222", "+49333"}}
	ev := events.DeployEvent{Stack: "api", Status: events.StatusRolledBack, DurationMs: 2000, Error: "bad"}
	req := mustFormat(t, f, ev)

	if got := req.URL.String(); got != "http://localhost:8020/v2/send" {
		t.Errorf("signal url = %s, want .../v2/send", got)
	}
	body := bodyOf(t, req)
	if body["number"] != "+49111" {
		t.Errorf("number = %v", body["number"])
	}
	rec, _ := body["recipients"].([]any)
	if len(rec) != 2 || rec[0] != "+49222" {
		t.Errorf("recipients = %v", body["recipients"])
	}
	if msg, _ := body["message"].(string); !strings.Contains(msg, "api") || !strings.Contains(msg, "rolled back") {
		t.Errorf("signal message missing fields: %q", body["message"])
	}
}

func TestSignalFormatter_RolledBackUnhealthyMessage(t *testing.T) {
	f := signalFormatter{base: "http://localhost:8020", number: "+49111", recipients: []string{"+49222"}}
	ev := events.DeployEvent{Stack: "api", Status: events.StatusRolledBackUnhealthy, DurationMs: 2000, Error: "probe timed out"}
	req := mustFormat(t, f, ev)

	body := bodyOf(t, req)
	msg, _ := body["message"].(string)
	if !strings.Contains(msg, "api") || !strings.Contains(msg, "still unhealthy") || !strings.Contains(msg, "probe timed out") {
		t.Errorf("rolled_back_unhealthy message missing fields: %q", msg)
	}
}

func TestSignalFormatter_PrefixPrependsMessage(t *testing.T) {
	f := signalFormatter{base: "http://localhost:8020", number: "+49111", recipients: []string{"+49222"}, prefix: "argoneon"}
	ev := events.DeployEvent{Stack: "jdownloader", Status: events.StatusSuccess, DurationMs: 3000}
	req := mustFormat(t, f, ev)

	msg, _ := bodyOf(t, req)["message"].(string)
	if !strings.HasPrefix(msg, "[argoneon] ") {
		t.Errorf("message should start with the host prefix, got %q", msg)
	}
	if !strings.Contains(msg, "jdownloader") {
		t.Errorf("prefixed message still carries the stack, got %q", msg)
	}
}

func TestSignalFormatter_EmptyPrefixIsOmitted(t *testing.T) {
	f := signalFormatter{base: "http://localhost:8020", number: "+49111", recipients: []string{"+49222"}}
	ev := events.DeployEvent{Stack: "web", Status: events.StatusSuccess, DurationMs: 3000}
	req := mustFormat(t, f, ev)

	if msg, _ := bodyOf(t, req)["message"].(string); strings.HasPrefix(msg, "[") {
		t.Errorf("no prefix configured, message must not start with a bracket, got %q", msg)
	}
}

func TestIsTerminal_IncludesRolledBackUnhealthy(t *testing.T) {
	if !isTerminal(events.StatusRolledBackUnhealthy) {
		t.Error("rolled_back_unhealthy must be a terminal status so it can be delivered")
	}
}

func TestGenericFormatter_EventBodyAndHeaders(t *testing.T) {
	f := genericFormatter{url: "https://ntfy.example/skipper", headers: map[string]string{"Authorization": "Bearer tok"}}
	ev := events.DeployEvent{Stack: "web", Status: events.StatusFailed, DurationMs: 1200, Error: "x"}
	req := mustFormat(t, f, ev)

	if got := req.Header.Get("Authorization"); got != "Bearer tok" {
		t.Errorf("Authorization = %q", got)
	}
	body := bodyOf(t, req)
	if body["stack"] != "web" || body["status"] != "failed" {
		t.Errorf("generic body missing event fields: %v", body)
	}
}

func TestGenericFormatter_StripsDiffs(t *testing.T) {
	f := genericFormatter{url: "https://x.example/y"}
	ev := events.DeployEvent{Stack: "web", Status: events.StatusSuccess, Diffs: map[string]string{"a": "huge"}}
	req := mustFormat(t, f, ev)

	if _, present := bodyOf(t, req)["diffs"]; present {
		t.Errorf("generic body should not carry diffs")
	}
}

func TestFormatterFor_UnknownFormat(t *testing.T) {
	if _, err := formatterFor(config.NotificationTarget{Format: "telegram", URL: "https://x"}); err == nil {
		t.Fatal("expected error for unknown format")
	}
}
