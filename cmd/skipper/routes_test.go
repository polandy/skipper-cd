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

// The headless core answers any Host — a forge or a probe reaches skipper by
// whatever name it has — while the UI and its API answer only an allowed one
// (DNS rebinding, ADR-0067). Every response carries the security headers.
func TestMainHandler_HostCheckGuardsOnlyTheUI(t *testing.T) {
	ok := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) })
	core := http.NewServeMux()
	core.Handle("POST /webhook", ok)
	core.Handle("GET /healthz", ok)
	app := http.NewServeMux()
	app.Handle("GET /api/v1/snapshot", ok)
	h := mainHandler(core, app, []string{"skipper.lan"})

	tests := []struct {
		method, path, host string
		want               int
	}{
		{http.MethodPost, "/webhook", "attacker.example", http.StatusOK},
		{http.MethodGet, "/healthz", "10.0.0.5:8080", http.StatusOK},
		{http.MethodGet, "/api/v1/snapshot", "attacker.example", http.StatusMisdirectedRequest},
		{http.MethodGet, "/api/v1/snapshot", "skipper.lan:8080", http.StatusOK},
	}
	for _, tt := range tests {
		req := httptest.NewRequest(tt.method, tt.path, nil)
		req.Host = tt.host
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code != tt.want {
			t.Errorf("%s %s (Host %s): status = %d, want %d", tt.method, tt.path, tt.host, rec.Code, tt.want)
		}
		if rec.Header().Get("Content-Security-Policy") == "" || rec.Header().Get("X-Frame-Options") != "DENY" {
			t.Errorf("%s %s (Host %s): security headers missing: %v", tt.method, tt.path, tt.host, rec.Header())
		}
	}
}

// Headless (UI off), only the core routes exist and nothing is host-checked.
func TestMainHandler_HeadlessServesCoreOnly(t *testing.T) {
	core := http.NewServeMux()
	core.Handle("GET /healthz", http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) }))
	h := mainHandler(core, nil, []string{"skipper.lan"})

	for path, want := range map[string]int{"/healthz": http.StatusOK, "/": http.StatusNotFound} {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		req.Host = "attacker.example"
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code != want {
			t.Errorf("GET %s: status = %d, want %d", path, rec.Code, want)
		}
	}
}
