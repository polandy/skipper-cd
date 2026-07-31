// Package ui provides HTTP handlers for the skipper-cd web interface,
// including SSE-based real-time deploy event streaming.
package ui

import (
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/polandy/skipper-cd/internal/audit"
	"github.com/polandy/skipper-cd/internal/events"
)

// VersionHandler serves GET /api/version — the build identity injected via
// -ldflags (and, for local builds, the commit stamped into the Go build info).
// Returns {"version","branch","commit"}; branch/commit may be empty. The header
// shows the branch for feature-branch builds, else the version, and appends the
// commit when present.
func VersionHandler(b BuildInfo) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, map[string]string{
			"version": b.Version,
			"branch":  b.Branch,
			"commit":  b.Commit,
		})
	})
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	// The status header is written implicitly; a failed body write cannot be
	// reported to the client anymore.
	_ = json.NewEncoder(w).Encode(v)
}

// DiffHandler serves GET /api/events/{id}/diffs — returns the diff content
// for a specific deploy event as JSON.
func DiffHandler(history *events.History) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		idStr := r.PathValue("id")
		id, err := strconv.ParseInt(idStr, 10, 64)
		if err != nil {
			http.Error(w, "invalid event id", http.StatusBadRequest)
			return
		}
		evt, ok := history.EventByID(id)
		if !ok {
			http.Error(w, "event not found", http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		// The status header is already written; a failed body write cannot
		// be reported to the client anymore.
		_ = json.NewEncoder(w).Encode(map[string]any{
			"diffs":   evt.Diffs,
			"commits": evt.Commits,
		})
	})
}

// AuditHandler serves the durable per-stack deploy audit log (ADR-0033) as a
// JSON array, newest first — registered on both GET /api/audit (the UI's
// route) and its versioned alias GET /api/v1/audit, the stable contract the
// multi-host fan-in polls (ADR-0039). With ?stack=<name> it returns that one
// stack's history; without it, recent records across all stacks. An optional
// ?limit=<n> caps the count.
func AuditHandler(log *audit.Log) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		limit := 0
		if v := r.URL.Query().Get("limit"); v != "" {
			if n, err := strconv.Atoi(v); err == nil && n > 0 {
				limit = n
			}
		}

		var records []audit.Record
		if stack := r.URL.Query().Get("stack"); stack != "" {
			records = log.Stack(stack, limit)
		} else {
			records = log.Recent(limit)
		}
		if records == nil {
			records = []audit.Record{} // encode [] not null for an empty history
		}

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(records)
	})
}
