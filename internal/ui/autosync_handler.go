package ui

import (
	"encoding/json"
	"log/slog"
	"net/http"

	"github.com/polandy/skipper-cd/internal/autosync"
)

// Autosync: the only write surface the UI has (a POST that toggles autosync)
// plus the queue it feeds. Guarded by RequireSameOrigin at the mux.

// autosyncPostRequest is the body of POST /api/autosync.
type autosyncPostRequest struct {
	Scope   string `json:"scope"` // "global" | "stack"
	Stack   string `json:"stack"` // required when scope == "stack"
	Enabled bool   `json:"enabled"`
}

// effectiveAfter reports the value the toggle actually produced. A per-stack
// override wins over the global switch in both directions (ADR-0019), so the
// requested value and the effective one usually agree — but reading it back
// from the controller is what makes the log a record of what happened rather
// than of what was asked for.
func effectiveAfter(ctrl *autosync.Controller, req autosyncPostRequest) bool {
	if req.Scope == "global" {
		return ctrl.GlobalEffective()
	}
	return ctrl.Effective(req.Stack)
}

// AutosyncHandler serves GET /api/autosync (current snapshot) and
// POST /api/autosync (set a runtime override). On a change it calls onChange to
// publish the new state; when the change enables sync it calls triggerDeploy to
// drain the queue. See docs/autosync.md.
func AutosyncHandler(ctrl *autosync.Controller, order func() []string, onChange, triggerDeploy func()) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			writeJSON(w, ctrl.Snapshot(order()))
		case http.MethodPost:
			var req autosyncPostRequest
			if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&req); err != nil {
				http.Error(w, "invalid request body", http.StatusBadRequest)
				return
			}
			enabled := req.Enabled
			switch req.Scope {
			case "global":
				ctrl.SetGlobal(&enabled)
			case "stack":
				if req.Stack == "" {
					http.Error(w, "stack is required when scope is \"stack\"", http.StatusBadRequest)
					return
				}
				ctrl.SetStack(req.Stack, &enabled)
			default:
				http.Error(w, "scope must be \"global\" or \"stack\"", http.StatusBadRequest)
				return
			}
			// A toggle changes whether a stack deploys at all, and until now it
			// left no trace: neither "who paused this" nor "did the write land"
			// was answerable from the log. Logged after the controller accepted
			// it, so the recorded effective value is the one that took.
			slog.Info("autosync set",
				"scope", req.Scope, "stack", req.Stack, "enabled", enabled,
				"effective", effectiveAfter(ctrl, req))
			if onChange != nil {
				onChange()
			}
			if req.Enabled && triggerDeploy != nil {
				triggerDeploy()
			}
			writeJSON(w, ctrl.Snapshot(order()))
		default:
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		}
	})
}

// QueueHandler serves GET /api/queue — the ordered pending list.
func QueueHandler(q *autosync.Queue, order func() []string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, q.View(order()))
	})
}
