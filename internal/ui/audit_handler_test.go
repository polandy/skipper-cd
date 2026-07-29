package ui

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/polandy/skipper-cd/internal/audit"
	"github.com/polandy/skipper-cd/internal/events"
)

func recordAt(log *audit.Log, stack string, status events.Status, minutes int) {
	log.Record(events.DeployEvent{
		Stack:     stack,
		Status:    status,
		Timestamp: time.Date(2026, 7, 18, 0, minutes, 0, 0, time.UTC),
	})
}

func decodeAudit(t *testing.T, h http.Handler, target string) []audit.Record {
	t.Helper()
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, target, nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); ct != "application/json" {
		t.Errorf("Content-Type = %q, want application/json", ct)
	}
	var out []audit.Record
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("decode: %v (body: %s)", err, rec.Body.String())
	}
	return out
}

func TestAuditHandler_FiltersByStackNewestFirst(t *testing.T) {
	log := audit.NewLog("")
	recordAt(log, "web", events.StatusSuccess, 1)
	recordAt(log, "db", events.StatusFailed, 2)
	recordAt(log, "web", events.StatusSuccess, 3)

	got := decodeAudit(t, AuditHandler(log), "/api/audit?stack=web")
	if len(got) != 2 {
		t.Fatalf("want 2 web records, got %d", len(got))
	}
	if got[0].Timestamp.Minute() != 3 || got[1].Timestamp.Minute() != 1 {
		t.Errorf("want newest-first (min 3 then 1), got %d then %d",
			got[0].Timestamp.Minute(), got[1].Timestamp.Minute())
	}
}

func TestAuditHandler_LimitCapsResults(t *testing.T) {
	log := audit.NewLog("")
	for i := 1; i <= 5; i++ {
		recordAt(log, "web", events.StatusSuccess, i)
	}
	got := decodeAudit(t, AuditHandler(log), "/api/audit?stack=web&limit=2")
	if len(got) != 2 {
		t.Fatalf("limit=2: got %d", len(got))
	}
}

func TestAuditHandler_NoStackReturnsRecentAcrossStacks(t *testing.T) {
	log := audit.NewLog("")
	recordAt(log, "a", events.StatusSuccess, 1)
	recordAt(log, "b", events.StatusSuccess, 2)

	got := decodeAudit(t, AuditHandler(log), "/api/audit")
	if len(got) != 2 {
		t.Fatalf("want 2 records across stacks, got %d", len(got))
	}
}

func TestAuditHandler_EmptyHistoryEncodesArrayNotNull(t *testing.T) {
	rec := httptest.NewRecorder()
	AuditHandler(audit.NewLog("")).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/audit?stack=nope", nil))
	if body := rec.Body.String(); body != "[]\n" {
		t.Errorf("empty history body = %q, want %q", body, "[]\n")
	}
}
