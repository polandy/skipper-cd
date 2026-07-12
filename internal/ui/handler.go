// Package ui provides HTTP handlers for the skipper-cd web interface,
// including SSE-based real-time deploy event streaming.
package ui

import (
	"embed"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/polandy/skipper-cd/internal/autosync"
	"github.com/polandy/skipper-cd/internal/events"
)

//go:embed static/index.html
var staticFS embed.FS

// IndexHandler serves the embedded UI HTML page.
func IndexHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		data, err := staticFS.ReadFile("static/index.html")
		if err != nil {
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write(data)
	})
}

// SSEHandler returns an HTTP handler that streams deploy events via
// Server-Sent Events. On connect it replays deploy history and, when a state
// broadcaster is configured, the current autosync and queue snapshots, then
// streams live deploy and state (autosync/queue) events. Supports
// Last-Event-ID for reconnection of the deploy history.
func SSEHandler(deployB *events.Broadcaster[events.DeployEvent], stateB *events.Broadcaster[events.StateEvent], history *events.History, initialState func() []events.StateEvent) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		flusher, ok := w.(http.Flusher)
		if !ok {
			http.Error(w, "streaming not supported", http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("Connection", "keep-alive")
		w.Header().Set("X-Accel-Buffering", "no")

		// Replay history (optionally filtered by Last-Event-ID).
		var historyEvents []events.DeployEvent
		if lastID := r.Header.Get("Last-Event-ID"); lastID != "" {
			if id, err := strconv.ParseInt(lastID, 10, 64); err == nil {
				historyEvents = history.EventsAfterID(id)
			}
		} else {
			historyEvents = history.Events()
		}

		for _, evt := range historyEvents {
			if err := writeSSE(w, evt); err != nil {
				return
			}
		}
		// Send the current autosync + queue snapshots so a fresh tab paints.
		if initialState != nil {
			for _, se := range initialState() {
				if err := writeSSEState(w, se); err != nil {
					return
				}
			}
		}
		flusher.Flush()

		// Subscribe to live deploy events, and state events when available.
		dch, unsubD := deployB.Subscribe()
		defer unsubD()
		var sch <-chan events.StateEvent
		if stateB != nil {
			var unsubS func()
			sch, unsubS = stateB.Subscribe()
			defer unsubS()
		}

		// Keepalive ticker.
		keepalive := time.NewTicker(30 * time.Second)
		defer keepalive.Stop()

		ctx := r.Context()
		for {
			select {
			case <-ctx.Done():
				return
			case evt := <-dch:
				if err := writeSSE(w, evt); err != nil {
					return
				}
				flusher.Flush()
			case se := <-sch: // sch is nil (blocks forever) when no state broadcaster
				if err := writeSSEState(w, se); err != nil {
					return
				}
				flusher.Flush()
			case <-keepalive.C:
				// A failed keepalive means the client is gone.
				if _, err := fmt.Fprintf(w, ": keepalive\n\n"); err != nil {
					return
				}
				flusher.Flush()
			}
		}
	})
}

// autosyncPostRequest is the body of POST /api/autosync.
type autosyncPostRequest struct {
	Scope   string `json:"scope"` // "global" | "stack"
	Stack   string `json:"stack"` // required when scope == "stack"
	Enabled bool   `json:"enabled"`
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

// VersionHandler serves GET /api/version — the build-time skipper-cd version
// injected via -ldflags. Returns {"version": "<semver>|dev"} for the UI header.
func VersionHandler(version string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, map[string]string{"version": version})
	})
}

// QueueHandler serves GET /api/queue — the ordered pending list.
func QueueHandler(q *autosync.Queue, order func() []string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, q.View(order()))
	})
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	// The status header is written implicitly; a failed body write cannot be
	// reported to the client anymore.
	_ = json.NewEncoder(w).Encode(v)
}

func writeSSEState(w http.ResponseWriter, se events.StateEvent) error {
	data, err := json.Marshal(se.Data)
	if err != nil {
		return err
	}
	_, err = fmt.Fprintf(w, "event: %s\ndata: %s\n\n", se.Name, data)
	return err
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
			"diffs": evt.Diffs,
		})
	})
}

func writeSSE(w http.ResponseWriter, evt events.DeployEvent) error {
	payload := evt.SSEPayload()
	data, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	_, err = fmt.Fprintf(w, "id: %d\nevent: deploy\ndata: %s\n\n", evt.ID, data)
	return err
}
