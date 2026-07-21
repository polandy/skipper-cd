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

	"github.com/polandy/skipper-cd/internal/audit"
	"github.com/polandy/skipper-cd/internal/autosync"
	"github.com/polandy/skipper-cd/internal/events"
)

//go:embed static/index.html static/app.css static/app-helpers.js static/manifest.webmanifest static/sw.js static/icons static/fonts
var staticFS embed.FS

// sseKeepaliveInterval is how often an idle SSE stream (deploy events, state
// events, logs) sends a keepalive so intermediary proxies don't time out the
// connection.
const sseKeepaliveInterval = 30 * time.Second

// IndexHandler serves the embedded UI HTML page, with the configured theme
// (see internal/ui/theme.go) baked into the data-theme attribute, favicon and
// PWA meta colours once at construction time — the same __PLACEHOLDER__
// substitution ServiceWorkerHandler uses for __VERSION__. themeSwitcher gates
// the in-UI theme picker: it is baked into data-theme-switcher, which the CSS
// and pre-paint/picker JS read to show (or hide) the picker and honour (or
// ignore) a saved per-browser override. Served via staticAsset, so a
// gzip-capable client gets the pre-compressed body (see compress.go).
func IndexHandler(theme string, themeSwitcher bool) http.Handler {
	data, err := staticFS.ReadFile("static/index.html")
	if err != nil {
		panic(err) // staticFS embeds static/index.html at compile time, so this cannot fail.
	}
	id := themeIdentityFor(theme)
	switcher := "off"
	if themeSwitcher {
		switcher = "on"
	}
	body := string(data)
	body = strings.ReplaceAll(body, "__UI_THEME__", theme)
	body = strings.ReplaceAll(body, "__THEME_SWITCHER__", switcher)
	body = strings.ReplaceAll(body, "__FAVICON_URI__", faviconDataURI(theme))
	body = strings.ReplaceAll(body, "__THEME_COLOR_DARK__", id.darkBase)
	body = strings.ReplaceAll(body, "__THEME_COLOR_LIGHT__", id.lightBase)
	asset := newStaticAsset("text/html; charset=utf-8", []byte(body))
	return http.HandlerFunc(asset.ServeHTTP)
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
	data, err := staticFS.ReadFile("static/sw.js")
	if err != nil {
		panic(err) // staticFS embeds static/sw.js at compile time, so this cannot fail.
	}
	body := []byte(strings.ReplaceAll(string(data), "__VERSION__", b.CacheID()))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/javascript; charset=utf-8")
		w.Header().Set("Cache-Control", "no-cache")
		_, _ = w.Write(body)
	})
}

// AppHelpersHandler serves GET /app-helpers.js — the pure, DOM-free UI helper
// functions extracted from the app shell (ADR-0035) so a node --test unit layer
// can import them without a build step. The app shell loads this first and calls
// the helpers as globals. Served no-cache so it stays in lockstep with the shell
// it supports; the service worker caches it in the app shell for offline use.
// Served via staticAsset, so a gzip-capable client gets the pre-compressed
// body (see compress.go).
func AppHelpersHandler() http.Handler {
	data, err := staticFS.ReadFile("static/app-helpers.js")
	if err != nil {
		panic(err) // staticFS embeds static/app-helpers.js at compile time, so this cannot fail.
	}
	asset := newStaticAsset("text/javascript; charset=utf-8", data)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-cache")
		asset.ServeHTTP(w, r)
	})
}

// AppCSSHandler serves GET /app.css — the app shell's stylesheet, extracted
// from index.html (ADR-0035 amendment) so the markup and script stay readable
// and diffs of styling changes don't interleave with unrelated ones. Served
// no-cache so it stays in lockstep with the shell it styles; the service
// worker caches it in the app shell for offline use. Served via staticAsset,
// so a gzip-capable client gets the pre-compressed body (see compress.go).
func AppCSSHandler() http.Handler {
	data, err := staticFS.ReadFile("static/app.css")
	if err != nil {
		panic(err) // staticFS embeds static/app.css at compile time, so this cannot fail.
	}
	asset := newStaticAsset("text/css; charset=utf-8", data)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-cache")
		asset.ServeHTTP(w, r)
	})
}

// IconsHandler serves the embedded PWA icons under /icons/ (used by the manifest
// and the apple-touch-icon link). Content types follow the file extension. The
// file server is scoped to static/icons and mounted with StripPrefix so it can
// only ever serve icons — it cannot reach the app-shell files (index.html,
// sw.js, the manifest) regardless of how it is routed.
func IconsHandler() http.Handler {
	sub, err := fs.Sub(staticFS, "static/icons")
	if err != nil {
		panic(err) // staticFS embeds static/icons at compile time, so this cannot fail.
	}
	return http.StripPrefix("/icons/", http.FileServerFS(sub))
}

// FontsHandler serves the self-hosted web fonts (DM Sans + JetBrains Mono, woff2)
// under /fonts/ from the embedded FS. Like IconsHandler it is scoped to
// static/fonts and mounted with StripPrefix, so it can only ever serve fonts —
// never the app-shell files. The woff2 content type is set explicitly because
// Go's mime table does not include it, and the files are served immutable with a
// long max-age: they are content-stable (a font change lands under a new name),
// and the service worker caches them under a per-build cache name anyway.
func FontsHandler() http.Handler {
	sub, err := fs.Sub(staticFS, "static/fonts")
	if err != nil {
		panic(err) // staticFS embeds static/fonts at compile time, so this cannot fail.
	}
	fileServer := http.StripPrefix("/fonts/", http.FileServerFS(sub))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, ".woff2") {
			w.Header().Set("Content-Type", "font/woff2")
			w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
		}
		fileServer.ServeHTTP(w, r)
	})
}

// SSEHandler returns an HTTP handler that streams deploy events via
// Server-Sent Events. On connect it replays deploy history and, when a state
// broadcaster is configured, the current autosync and queue snapshots, then
// streams live deploy and state (autosync/queue) events. Supports
// Last-Event-ID for reconnection of the deploy history.
// The initial UI state (autosync, stacks, health, …) is no longer replayed on
// this stream — the UI fetches it from GET /api/v1/snapshot on every (re)open
// (ADR-0039). This stream replays the deploy-event history and then carries
// live deploy events plus live state changes.
func SSEHandler(deployB *events.Broadcaster[events.DeployEvent], stateB *events.Broadcaster[events.StateEvent], history *events.History) http.Handler {
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
		keepalive := time.NewTicker(sseKeepaliveInterval)
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

// SnapshotHandler serves GET /api/v1/snapshot — the current UI state as one
// JSON object keyed by state-event name, e.g.
// {"stacks": {...}, "health": {...}, "autosync": {...}, ...}. The values are
// the same payloads the SSE stream publishes under those event names, built
// from the same collector, so the REST snapshot and the live stream cannot
// drift (ADR-0039). It is the read surface external consumers poll — notably
// the multi-host fan-in (ADR-0048) — and the UI's own initial paint, replacing
// the former SSE initial-state burst. `collect` may refresh live subsystems as
// a side effect (the same refresh a fresh SSE connection triggers).
func SnapshotHandler(collect func() []events.StateEvent) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		snap := make(map[string]any, len(events.AllStateNames))
		for _, se := range collect() {
			snap[se.Name] = se.Data
		}
		w.Header().Set("Content-Type", "application/json")
		// The status header is implicitly 200; a failed body write cannot be
		// reported to the client anymore.
		_ = json.NewEncoder(w).Encode(snap)
	})
}

// PeersHandler serves GET /api/peers — the effective multi-host set (ADR-0048)
// as JSON: the primary itself first (self:true), then each configured peer,
// with reachability and last-seen but without the bulky per-host read data
// (that rides the `peers` state event). `hosts` returns the live set; it is
// passed as an opaque payload so this package need not import internal/peers
// (which would cycle through config → ui).
func PeersHandler(hosts func() any) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		// The status header is implicitly 200; a failed body write cannot be
		// reported to the client anymore.
		_ = json.NewEncoder(w).Encode(map[string]any{"hosts": hosts()})
	})
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

// AuditHandler serves GET /api/audit — the durable per-stack deploy audit log
// (ADR-0033) as a JSON array, newest first. With ?stack=<name> it returns that
// one stack's history; without it, recent records across all stacks. An
// optional ?limit=<n> caps the count.
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

func writeSSE(w http.ResponseWriter, evt events.DeployEvent) error {
	payload := evt.SSEPayload()
	data, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	_, err = fmt.Fprintf(w, "id: %d\nevent: deploy\ndata: %s\n\n", evt.ID, data)
	return err
}
