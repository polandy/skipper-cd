package ui

import (
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/polandy/skipper-cd/internal/logbuf"
)

func TestLogsSSEHandler_SendsBacklogOnConnect(t *testing.T) {
	log := logbuf.New(10)
	log.Append(time.Now(), "INFO", "starting up", nil)
	log.Append(time.Now(), "WARN", "disk almost full", nil)

	handler := LogsSSEHandler(log)
	req := httptest.NewRequest(http.MethodGet, "/api/logs", nil)
	rec := serveSSE(t, handler, req, nil)

	body := rec.Body.String()
	if !strings.Contains(body, `"msg":"starting up"`) {
		t.Error("expected backlog entry 'starting up'")
	}
	if !strings.Contains(body, `"msg":"disk almost full"`) {
		t.Error("expected backlog entry 'disk almost full'")
	}
}

func TestLogsSSEHandler_FiltersBacklogByLastEventID(t *testing.T) {
	log := logbuf.New(10)
	log.Append(time.Now(), "INFO", "old line", nil)
	log.Append(time.Now(), "INFO", "new line", nil)
	oldID := log.Entries()[0].ID

	handler := LogsSSEHandler(log)
	req := httptest.NewRequest(http.MethodGet, "/api/logs", nil)
	req.Header.Set("Last-Event-ID", strconv.FormatInt(oldID, 10))
	rec := serveSSE(t, handler, req, nil)

	body := rec.Body.String()
	if strings.Contains(body, `"msg":"old line"`) {
		t.Error("should not contain entries before Last-Event-ID")
	}
	if !strings.Contains(body, `"msg":"new line"`) {
		t.Error("expected entry with ID > Last-Event-ID")
	}
}

func TestLogsSSEHandler_StreamsLiveEntries(t *testing.T) {
	log := logbuf.New(10)

	handler := LogsSSEHandler(log)
	req := httptest.NewRequest(http.MethodGet, "/api/logs", nil)
	rec := serveSSE(t, handler, req, func() {
		log.ChildLine("docker", "stdout", "Container gitea Started", "")
	})

	body := rec.Body.String()
	if !strings.Contains(body, `"msg":"Container gitea Started"`) {
		t.Errorf("expected live entry in body: %s", body)
	}
	if !strings.Contains(body, `"cmd":"docker"`) {
		t.Error("expected cmd attr in payload")
	}
}

func TestLogsSSEHandler_DoesNotDuplicateEntriesAcrossReplayAndLive(t *testing.T) {
	log := logbuf.New(10)
	log.Append(time.Now(), "INFO", "backlog line", nil)

	handler := LogsSSEHandler(log)
	req := httptest.NewRequest(http.MethodGet, "/api/logs", nil)
	rec := serveSSE(t, handler, req, func() {
		log.Append(time.Now(), "INFO", "live line", nil)
	})

	body := rec.Body.String()
	if got := strings.Count(body, `"msg":"backlog line"`); got != 1 {
		t.Errorf("expected the backlog entry exactly once, got %d times", got)
	}
	if got := strings.Count(body, `"msg":"live line"`); got != 1 {
		t.Errorf("expected the live entry exactly once, got %d times", got)
	}
}

func TestLogsSSEHandler_SetsSSEHeaders(t *testing.T) {
	handler := LogsSSEHandler(logbuf.New(10))
	req := httptest.NewRequest(http.MethodGet, "/api/logs", nil)
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

func TestIndexHandler_ContainsLogsViewToggle(t *testing.T) {
	handler := IndexHandler(ThemeCatppuccin, false)
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	body := rec.Body.String()
	for _, want := range []string{`id="view-toggle"`, `id="log-pane"`, "/api/logs", `id="follow-logs"`, "log-diff-pill"} {
		if !strings.Contains(body, want) {
			t.Errorf("expected index.html to contain %q", want)
		}
	}
}

func TestWriteLogSSE_Format(t *testing.T) {
	rec := httptest.NewRecorder()
	entry := logbuf.Entry{ID: 42, Level: "INFO", Msg: "hello"}

	if err := writeLogSSE(rec, entry); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	body := rec.Body.String()
	if !strings.HasPrefix(body, "id: 42\n") {
		t.Error("expected SSE id line")
	}
	if !strings.Contains(body, "event: log\n") {
		t.Error("expected SSE event type 'log'")
	}
	if !strings.Contains(body, "data: ") {
		t.Error("expected SSE data line")
	}
	if !strings.HasSuffix(body, "\n\n") {
		t.Error("expected SSE double newline terminator")
	}
}
