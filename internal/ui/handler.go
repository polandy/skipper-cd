// Package ui provides HTTP handlers for the skipper-cd web interface,
// including SSE-based real-time deploy event streaming.
package ui

import (
	"embed"
	"encoding/json"
	"fmt"
	"io/fs"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/polandy/skipper-cd/internal/autosync"
	"github.com/polandy/skipper-cd/internal/events"
)

//go:embed static/index.html static/manifest.webmanifest static/sw.js static/icons
var staticFS embed.FS

// IndexHandler serves the embedded UI HTML page, with the configured theme
// (see internal/ui/theme.go) baked into the data-theme attribute, favicon and
// PWA meta colours once at construction time — the same __PLACEHOLDER__
// substitution ServiceWorkerHandler uses for __VERSION__.
func IndexHandler(theme string) http.Handler {
	data, err := staticFS.ReadFile("static/index.html")
	if err != nil {
		panic(err) // staticFS embeds static/index.html at compile time, so this cannot fail.
	}
	id := themeIdentityFor(theme)
	body := string(data)
	body = strings.ReplaceAll(body, "__UI_THEME__", theme)
	body = strings.ReplaceAll(body, "__FAVICON_URI__", faviconDataURI(theme))
	body = strings.ReplaceAll(body, "__THEME_COLOR_DARK__", id.darkBase)
	body = strings.ReplaceAll(body, "__THEME_COLOR_LIGHT__", id.lightBase)
	out := []byte(body)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write(out)
	})
}

// ManifestHandler serves GET /manifest.webmanifest — the PWA web app manifest
// that makes the UI installable. theme_color/background_color follow the
// configured theme's dark palette (a manifest cannot respond to a
// prefers-color-scheme media query, so it always reflects the dark variant,
// same as before this was made theme-aware). See docs/pwa.md.
func ManifestHandler(theme string) http.Handler {
	data, err := staticFS.ReadFile("static/manifest.webmanifest")
	if err != nil {
		panic(err) // staticFS embeds static/manifest.webmanifest at compile time, so this cannot fail.
	}
	id := themeIdentityFor(theme)
	body := string(data)
	body = strings.ReplaceAll(body, "__THEME_COLOR__", id.darkBase)
	body = strings.ReplaceAll(body, "__BACKGROUND_COLOR__", id.darkMantle)
	out := []byte(body)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/manifest+json; charset=utf-8")
		_, _ = w.Write(out)
	})
}

// BuildInfo carries the build identity surfaced in the UI header and used to
// cache-bust the service worker. Version is the release semver (or "dev");
// Branch and Commit are empty when the build path does not know them — the Nix
// flake, for instance, knows the commit but not the branch name.
type BuildInfo struct {
	Version string
	Branch  string
	Commit  string
}

// CacheID returns the identity baked into the service worker's cache name so a
// new build changes the served bytes. It folds in Commit because two
// feature-branch builds share the same release semver — without the commit
// the app-shell cache would not bust between them.
func (b BuildInfo) CacheID() string {
	if b.Commit == "" {
		return b.Version
	}
	return b.Version + "-" + b.Commit
}

// ServiceWorkerHandler serves GET /sw.js — the PWA service worker. The build
// cache id is baked into the worker's cache name (replacing the __VERSION__
// placeholder) so a new build changes the served bytes and the browser adopts
// a fresh worker. Served no-cache so worker updates always propagate.
func ServiceWorkerHandler(b BuildInfo) http.Handler {
	data, _ := staticFS.ReadFile("static/sw.js")
	body := []byte(strings.ReplaceAll(string(data), "__VERSION__", b.CacheID()))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/javascript; charset=utf-8")
		w.Header().Set("Cache-Control", "no-cache")
		_, _ = w.Write(body)
	})
}

// IconsHandler serves the embedded PWA icons under /icons/ (used by the manifest
// and the apple-touch-icon link). Content types follow the file extension.
func IconsHandler() http.Handler {
	sub, err := fs.Sub(staticFS, "static")
	if err != nil {
		// staticFS embeds static/ at compile time, so this cannot fail.
		panic(err)
	}
	return http.FileServerFS(sub)
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
