package ui

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/polandy/skipper-cd/internal/events"
)

// serveSSE runs the blocking SSE handler in a goroutine, gives it time to
// write, optionally runs `during` (e.g. publishing a live event), then
// cancels the request and waits for the handler to exit. Reading the
// recorder afterwards is race-free because the handler has returned.
func serveSSE(t *testing.T, handler http.Handler, req *http.Request, during func()) *httptest.ResponseRecorder {
	t.Helper()
	ctx, cancel := context.WithCancel(req.Context())
	defer cancel()
	req = req.WithContext(ctx)
	rec := httptest.NewRecorder()

	done := make(chan struct{})
	go func() {
		handler.ServeHTTP(rec, req)
		close(done)
	}()

	time.Sleep(50 * time.Millisecond)
	if during != nil {
		during()
		time.Sleep(50 * time.Millisecond)
	}
	cancel()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("SSE handler did not exit after context cancel")
	}
	return rec
}

func TestIndexHandler_ServesHTML(t *testing.T) {
	handler := IndexHandler()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rec.Code)
	}
	ct := rec.Header().Get("Content-Type")
	if !strings.Contains(ct, "text/html") {
		t.Errorf("expected text/html content type, got %q", ct)
	}
	body := rec.Body.String()
	if !strings.Contains(body, "skipper-cd") {
		t.Error("expected HTML to contain 'skipper-cd'")
	}
}

func TestSSEHandler_SendsHistoryOnConnect(t *testing.T) {
	broadcaster := events.NewBroadcaster()
	history := events.NewHistory("")
	history.Add(events.DeployEvent{ID: 1, Stack: "gitea", Status: events.StatusSuccess})
	history.Add(events.DeployEvent{ID: 2, Stack: "traefik", Status: events.StatusFailed, Error: "timeout"})

	handler := SSEHandler(broadcaster, nil, history, nil)
	req := httptest.NewRequest(http.MethodGet, "/api/events", nil)
	rec := serveSSE(t, handler, req, nil)

	body := rec.Body.String()
	if !strings.Contains(body, `"stack":"gitea"`) {
		t.Error("expected history event for gitea")
	}
	if !strings.Contains(body, `"stack":"traefik"`) {
		t.Error("expected history event for traefik")
	}
}

func TestSSEHandler_FiltersHistoryByLastEventID(t *testing.T) {
	broadcaster := events.NewBroadcaster()
	history := events.NewHistory("")
	history.Add(events.DeployEvent{ID: 1, Stack: "old"})
	history.Add(events.DeployEvent{ID: 2, Stack: "new"})

	handler := SSEHandler(broadcaster, nil, history, nil)
	req := httptest.NewRequest(http.MethodGet, "/api/events", nil)
	req.Header.Set("Last-Event-ID", "1")
	rec := serveSSE(t, handler, req, nil)

	body := rec.Body.String()
	if strings.Contains(body, `"stack":"old"`) {
		t.Error("should not contain events before Last-Event-ID")
	}
	if !strings.Contains(body, `"stack":"new"`) {
		t.Error("expected event with ID > Last-Event-ID")
	}
}

func TestSSEHandler_StreamsLiveEvents(t *testing.T) {
	broadcaster := events.NewBroadcaster()
	history := events.NewHistory("")

	handler := SSEHandler(broadcaster, nil, history, nil)
	req := httptest.NewRequest(http.MethodGet, "/api/events", nil)
	rec := serveSSE(t, handler, req, func() {
		broadcaster.Publish(events.DeployEvent{
			ID:     10,
			Stack:  "monitoring",
			Status: events.StatusDeploying,
		})
	})

	body := rec.Body.String()
	if !strings.Contains(body, `"stack":"monitoring"`) {
		t.Errorf("expected live event for monitoring in body: %s", body)
	}
	if !strings.Contains(body, "id: 10") {
		t.Error("expected SSE id: 10 in output")
	}
}

func TestSSEHandler_SetsCorrectHeaders(t *testing.T) {
	broadcaster := events.NewBroadcaster()
	history := events.NewHistory("")

	handler := SSEHandler(broadcaster, nil, history, nil)
	req := httptest.NewRequest(http.MethodGet, "/api/events", nil)
	rec := serveSSE(t, handler, req, nil)

	headers := rec.Header()
	if headers.Get("Content-Type") != "text/event-stream" {
		t.Errorf("expected text/event-stream, got %q", headers.Get("Content-Type"))
	}
	if headers.Get("Cache-Control") != "no-cache" {
		t.Errorf("expected no-cache, got %q", headers.Get("Cache-Control"))
	}
	if headers.Get("X-Accel-Buffering") != "no" {
		t.Errorf("expected X-Accel-Buffering=no, got %q", headers.Get("X-Accel-Buffering"))
	}
}

func TestDiffHandler_ReturnsNotFoundForUnknownID(t *testing.T) {
	history := events.NewHistory("")
	history.Add(events.DeployEvent{ID: 1, Stack: "gitea"})

	handler := DiffHandler(history)
	req := httptest.NewRequest(http.MethodGet, "/api/events/999/diffs", nil)
	req.SetPathValue("id", "999")
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Errorf("expected 404, got %d", rec.Code)
	}
}

func TestDiffHandler_ReturnsDiffsForKnownEvent(t *testing.T) {
	history := events.NewHistory("")
	history.Add(events.DeployEvent{
		ID:    1,
		Stack: "gitea",
		Diffs: map[string]string{"docker-compose.yml": "+new line"},
	})

	handler := DiffHandler(history)
	req := httptest.NewRequest(http.MethodGet, "/api/events/1/diffs", nil)
	req.SetPathValue("id", "1")
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rec.Code)
	}
	ct := rec.Header().Get("Content-Type")
	if !strings.Contains(ct, "application/json") {
		t.Errorf("expected application/json, got %q", ct)
	}
	body := rec.Body.String()
	if !strings.Contains(body, "docker-compose.yml") {
		t.Error("expected diff content in response")
	}
	if !strings.Contains(body, "+new line") {
		t.Error("expected diff text in response")
	}
}

func TestDiffHandler_ReturnsNullDiffsForEventWithoutDiffs(t *testing.T) {
	history := events.NewHistory("")
	history.Add(events.DeployEvent{ID: 1, Stack: "gitea"})

	handler := DiffHandler(history)
	req := httptest.NewRequest(http.MethodGet, "/api/events/1/diffs", nil)
	req.SetPathValue("id", "1")
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rec.Code)
	}
	body := rec.Body.String()
	if !strings.Contains(body, `"diffs":null`) {
		t.Errorf("expected null diffs, got %s", body)
	}
}

func TestDiffHandler_InvalidID(t *testing.T) {
	history := events.NewHistory("")
	handler := DiffHandler(history)
	req := httptest.NewRequest(http.MethodGet, "/api/events/abc/diffs", nil)
	req.SetPathValue("id", "abc")
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", rec.Code)
	}
}

func TestSSEHandler_StripsDiffsFromStream(t *testing.T) {
	broadcaster := events.NewBroadcaster()
	history := events.NewHistory("")
	history.Add(events.DeployEvent{
		ID:     1,
		Stack:  "gitea",
		Status: events.StatusSuccess,
		Diffs:  map[string]string{"file.yml": "+added"},
	})

	handler := SSEHandler(broadcaster, nil, history, nil)
	req := httptest.NewRequest(http.MethodGet, "/api/events", nil)
	rec := serveSSE(t, handler, req, nil)

	body := rec.Body.String()
	if strings.Contains(body, "+added") {
		t.Error("SSE stream should not contain diff content")
	}
	if !strings.Contains(body, `"has_diffs":true`) {
		t.Error("SSE stream should contain has_diffs flag")
	}
}

func TestWriteSSE_Format(t *testing.T) {
	rec := httptest.NewRecorder()
	evt := events.DeployEvent{
		ID:     42,
		Stack:  "gitea",
		Status: events.StatusSuccess,
	}

	if err := writeSSE(rec, evt); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	body := rec.Body.String()
	if !strings.HasPrefix(body, "id: 42\n") {
		t.Error("expected SSE id line")
	}
	if !strings.Contains(body, "event: deploy\n") {
		t.Error("expected SSE event type")
	}
	if !strings.Contains(body, "data: ") {
		t.Error("expected SSE data line")
	}
	if !strings.HasSuffix(body, "\n\n") {
		t.Error("expected SSE double newline terminator")
	}
}
