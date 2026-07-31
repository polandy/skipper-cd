package main

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/polandy/skipper-cd/internal/audit"
	"github.com/polandy/skipper-cd/internal/events"
	"github.com/polandy/skipper-cd/internal/logbuf"
)

// The audit log is served both on the legacy UI route and on the versioned
// /api/v1 alias the multi-host fan-in polls (ADR-0039 amendment): same
// handler, so the two can never diverge.
func TestRegisterEventRoutes_ServesAuditOnVersionedAndLegacyPaths(t *testing.T) {
	auditLog := audit.NewLog("")
	auditLog.Record(events.DeployEvent{
		Stack:     "web",
		Status:    events.StatusSuccess,
		Timestamp: time.Date(2026, 7, 31, 12, 0, 0, 0, time.UTC),
	})

	mux := http.NewServeMux()
	snap := stateSnapshot{autosync: &autosyncDeps{stateB: events.NewStateBroadcaster()}}
	registerEventRoutes(mux, events.NewBroadcaster(), events.NewHistory(t.TempDir()), auditLog, logbuf.New(8), snap)

	bodies := make(map[string]string, 2)
	for _, path := range []string{"/api/audit", "/api/v1/audit"} {
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path+"?stack=web", nil))
		if rec.Code != http.StatusOK {
			t.Fatalf("GET %s: status = %d, want 200", path, rec.Code)
		}
		bodies[path] = rec.Body.String()
	}
	if bodies["/api/audit"] != bodies["/api/v1/audit"] {
		t.Errorf("versioned and legacy audit bodies differ:\n legacy: %s\n v1:     %s",
			bodies["/api/audit"], bodies["/api/v1/audit"])
	}
}
