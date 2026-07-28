package ui

import (
	"compress/gzip"
	"context"
	"encoding/json"
	"fmt"
	"io"
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
	handler := IndexHandler(ThemeCatppuccin, false)
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

func TestIndexHandler_ServesGzipWhenAccepted(t *testing.T) {
	handler := IndexHandler(ThemeCatppuccin, false)
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Accept-Encoding", "gzip")
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if enc := rec.Header().Get("Content-Encoding"); enc != "gzip" {
		t.Fatalf("Content-Encoding = %q, want gzip", enc)
	}
	if vary := rec.Header().Get("Vary"); vary != "Accept-Encoding" {
		t.Errorf("Vary = %q, want Accept-Encoding", vary)
	}
	gr, err := gzip.NewReader(rec.Body)
	if err != nil {
		t.Fatalf("gzip.NewReader: %v", err)
	}
	got, err := io.ReadAll(gr)
	if err != nil {
		t.Fatalf("reading gzip body: %v", err)
	}
	if !strings.Contains(string(got), "skipper-cd") {
		t.Error("decompressed body does not contain 'skipper-cd'")
	}
}

func TestVersionHandler_ReturnsBuildInfo(t *testing.T) {
	tests := []struct {
		name string
		in   BuildInfo
	}{
		{"release", BuildInfo{Version: "1.2.3"}},
		{"feature branch", BuildInfo{Version: "1.2.3", Branch: "fix/mobile-header", Commit: "a1b2c3d"}},
		{"nix (commit only)", BuildInfo{Version: "1.2.3", Commit: "a1b2c3d"}},
		{"dev", BuildInfo{Version: "dev", Commit: "a1b2c3d"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			handler := VersionHandler(tt.in)
			req := httptest.NewRequest(http.MethodGet, "/api/version", nil)
			rec := httptest.NewRecorder()

			handler.ServeHTTP(rec, req)

			if rec.Code != http.StatusOK {
				t.Fatalf("status = %d, want 200", rec.Code)
			}
			var got struct {
				Version string `json:"version"`
				Branch  string `json:"branch"`
				Commit  string `json:"commit"`
			}
			if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
				t.Fatalf("decode: %v", err)
			}
			if got.Version != tt.in.Version {
				t.Errorf("version = %q, want %q", got.Version, tt.in.Version)
			}
			if got.Branch != tt.in.Branch {
				t.Errorf("branch = %q, want %q", got.Branch, tt.in.Branch)
			}
			if got.Commit != tt.in.Commit {
				t.Errorf("commit = %q, want %q", got.Commit, tt.in.Commit)
			}
		})
	}
}

func TestBuildInfo_CacheID(t *testing.T) {
	tests := []struct {
		name string
		in   BuildInfo
		want string
	}{
		{"version only", BuildInfo{Version: "1.2.3"}, "1.2.3"},
		{"version and commit", BuildInfo{Version: "1.2.3", Commit: "a1b2c3d"}, "1.2.3-a1b2c3d"},
		{"branch build shares semver", BuildInfo{Version: "1.2.3", Branch: "feat/x", Commit: "deadbee"}, "1.2.3-deadbee"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.in.CacheID(); got != tt.want {
				t.Errorf("CacheID() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestIndexHandler_BakesInConfiguredTheme(t *testing.T) {
	for _, theme := range ValidThemes {
		t.Run(theme, func(t *testing.T) {
			handler := IndexHandler(theme, false)
			req := httptest.NewRequest(http.MethodGet, "/", nil)
			rec := httptest.NewRecorder()

			handler.ServeHTTP(rec, req)

			body := rec.Body.String()
			if !strings.Contains(body, `data-theme="`+theme+`"`) {
				t.Errorf("expected data-theme=%q in served HTML", theme)
			}
			if !strings.Contains(body, `data-server-theme="`+theme+`"`) {
				t.Errorf("expected data-server-theme=%q in served HTML", theme)
			}
			for _, placeholder := range []string{"__UI_THEME__", "__THEME_SWITCHER__", "__FAVICON_URI__", "__THEME_COLOR_DARK__", "__THEME_COLOR_LIGHT__"} {
				if strings.Contains(body, placeholder) {
					t.Errorf("served HTML still contains the %s placeholder", placeholder)
				}
			}
			if !strings.Contains(body, "data:image/svg+xml;base64,") {
				t.Error("expected an inlined favicon data URI")
			}
		})
	}
}

func TestIndexHandler_BakesInThemeSwitcherFlag(t *testing.T) {
	for _, tc := range []struct {
		name    string
		enabled bool
		want    string
	}{
		{"disabled", false, `data-theme-switcher="off"`},
		{"enabled", true, `data-theme-switcher="on"`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			handler := IndexHandler(ThemeCatppuccin, tc.enabled)
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))

			body := rec.Body.String()
			if !strings.Contains(body, tc.want) {
				t.Errorf("expected %q in served HTML for enabled=%v", tc.want, tc.enabled)
			}
			if strings.Contains(body, "__THEME_SWITCHER__") {
				t.Error("served HTML still contains the __THEME_SWITCHER__ placeholder")
			}
		})
	}
}

func TestManifestHandler_ServesInstallableManifest(t *testing.T) {
	handler := ManifestHandler(ThemeCatppuccin)
	req := httptest.NewRequest(http.MethodGet, "/manifest.webmanifest", nil)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); !strings.Contains(ct, "application/manifest+json") {
		t.Errorf("content type = %q, want application/manifest+json", ct)
	}
	var m struct {
		Name    string `json:"name"`
		Display string `json:"display"`
		Icons   []struct {
			Src     string `json:"src"`
			Purpose string `json:"purpose"`
		} `json:"icons"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &m); err != nil {
		t.Fatalf("manifest is not valid JSON: %v", err)
	}
	if m.Display != "standalone" {
		t.Errorf("display = %q, want standalone", m.Display)
	}
	if len(m.Icons) == 0 {
		t.Error("manifest has no icons")
	}
	var hasMaskable bool
	for _, ic := range m.Icons {
		if ic.Purpose == "maskable" {
			hasMaskable = true
		}
	}
	if !hasMaskable {
		t.Error("manifest has no maskable icon")
	}
}

func TestManifestHandler_ThemeColorsFollowConfiguredTheme(t *testing.T) {
	handler := ManifestHandler(ThemeNord)
	req := httptest.NewRequest(http.MethodGet, "/manifest.webmanifest", nil)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	var m struct {
		ThemeColor      string `json:"theme_color"`
		BackgroundColor string `json:"background_color"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &m); err != nil {
		t.Fatalf("manifest is not valid JSON: %v", err)
	}
	nord := themeIdentities[ThemeNord]
	if m.ThemeColor != nord.darkBase {
		t.Errorf("theme_color = %q, want %q (Nord dark base)", m.ThemeColor, nord.darkBase)
	}
	if m.BackgroundColor != nord.darkMantle {
		t.Errorf("background_color = %q, want %q (Nord dark mantle)", m.BackgroundColor, nord.darkMantle)
	}
}

func TestServiceWorkerHandler_InjectsCacheID(t *testing.T) {
	handler := ServiceWorkerHandler(BuildInfo{Version: "9.9.9", Commit: "cafef00d"})
	req := httptest.NewRequest(http.MethodGet, "/sw.js", nil)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); !strings.Contains(ct, "javascript") {
		t.Errorf("content type = %q, want a javascript type", ct)
	}
	if cc := rec.Header().Get("Cache-Control"); cc != "no-cache" {
		t.Errorf("Cache-Control = %q, want no-cache", cc)
	}
	body := rec.Body.String()
	if strings.Contains(body, "__VERSION__") {
		t.Error("service worker still contains the __VERSION__ placeholder")
	}
	// The commit must be part of the cache id so two feature-branch builds that
	// share a release semver still bust the app-shell cache.
	if !strings.Contains(body, "9.9.9-cafef00d") {
		t.Error("service worker does not contain the version-commit cache id")
	}
}

func TestIconsHandler_ServesPNG(t *testing.T) {
	handler := IconsHandler()
	req := httptest.NewRequest(http.MethodGet, "/icons/icon-192.png", nil)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); !strings.Contains(ct, "image/png") {
		t.Errorf("content type = %q, want image/png", ct)
	}
	if got := rec.Body.Bytes(); len(got) < 8 || string(got[1:4]) != "PNG" {
		t.Error("body does not start with the PNG magic bytes")
	}
}

// TestIconsHandler_DoesNotServeAppShell locks in the encapsulation guarantee:
// the icons file server is scoped to static/icons, so it cannot serve app-shell
// files (index.html, sw.js, the manifest) even for a path that reaches it. This
// must not rely on the router restricting it to /icons/ — the handler owns its
// own boundary.
func TestIconsHandler_DoesNotServeAppShell(t *testing.T) {
	handler := IconsHandler()
	for _, path := range []string{"/index.html", "/sw.js", "/manifest.webmanifest"} {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		rec := httptest.NewRecorder()

		handler.ServeHTTP(rec, req)

		if rec.Code != http.StatusNotFound {
			t.Errorf("GET %s = %d, want 404 (icons handler must not serve app-shell files)", path, rec.Code)
		}
	}
}

func TestFontsHandler_ServesWoff2(t *testing.T) {
	handler := FontsHandler()
	req := httptest.NewRequest(http.MethodGet, "/fonts/dm-sans-400.woff2", nil)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); ct != "font/woff2" {
		t.Errorf("content type = %q, want font/woff2", ct)
	}
	if cc := rec.Header().Get("Cache-Control"); !strings.Contains(cc, "immutable") {
		t.Errorf("cache-control = %q, want an immutable long-lived cache", cc)
	}
	if got := rec.Body.Bytes(); len(got) < 4 || string(got[0:4]) != "wOF2" {
		t.Error("body does not start with the woff2 signature (wOF2)")
	}
}

// TestFontsHandler_DoesNotServeAppShell mirrors the icons guarantee: the fonts
// file server is scoped to static/fonts, so it cannot serve app-shell files even
// for a path that reaches it, independent of how the router mounts it.
func TestFontsHandler_DoesNotServeAppShell(t *testing.T) {
	handler := FontsHandler()
	for _, path := range []string{"/index.html", "/sw.js", "/manifest.webmanifest"} {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		rec := httptest.NewRecorder()

		handler.ServeHTTP(rec, req)

		if rec.Code != http.StatusNotFound {
			t.Errorf("GET %s = %d, want 404 (fonts handler must not serve app-shell files)", path, rec.Code)
		}
	}
}

func TestAppHelpersHandler_ServesJS(t *testing.T) {
	handler := AppHelpersHandler()
	req := httptest.NewRequest(http.MethodGet, "/app-helpers.js", nil)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); !strings.Contains(ct, "text/javascript") {
		t.Errorf("content type = %q, want text/javascript", ct)
	}
	if cc := rec.Header().Get("Cache-Control"); cc != "no-cache" {
		t.Errorf("cache-control = %q, want no-cache (lockstep with the app shell)", cc)
	}
	if body := rec.Body.String(); !strings.Contains(body, "function formatDuration") {
		t.Error("body does not contain the extracted helper functions")
	}
}

func TestAppJSHandler_ServesJS(t *testing.T) {
	handler := AppJSHandler()
	req := httptest.NewRequest(http.MethodGet, "/app.js", nil)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); !strings.Contains(ct, "text/javascript") {
		t.Errorf("content type = %q, want text/javascript", ct)
	}
	if cc := rec.Header().Get("Cache-Control"); cc != "no-cache" {
		t.Errorf("cache-control = %q, want no-cache (lockstep with the app shell)", cc)
	}
	if body := rec.Body.String(); !strings.Contains(body, "document.getElementById('tbody')") {
		t.Error("body does not contain the extracted app script")
	}
}

func TestAppJSHandler_ServesGzipWhenAccepted(t *testing.T) {
	handler := AppJSHandler()
	req := httptest.NewRequest(http.MethodGet, "/app.js", nil)
	req.Header.Set("Accept-Encoding", "gzip")
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if enc := rec.Header().Get("Content-Encoding"); enc != "gzip" {
		t.Fatalf("Content-Encoding = %q, want gzip", enc)
	}
	gr, err := gzip.NewReader(rec.Body)
	if err != nil {
		t.Fatalf("gzip.NewReader: %v", err)
	}
	got, err := io.ReadAll(gr)
	if err != nil {
		t.Fatalf("reading gzip body: %v", err)
	}
	if !strings.Contains(string(got), "document.getElementById('tbody')") {
		t.Error("gzip body does not contain the extracted app script")
	}
}

func TestAppRenderJSHandler_ServesJS(t *testing.T) {
	handler := AppRenderJSHandler()
	req := httptest.NewRequest(http.MethodGet, "/app-render.js", nil)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); !strings.Contains(ct, "text/javascript") {
		t.Errorf("content type = %q, want text/javascript", ct)
	}
	if cc := rec.Header().Get("Cache-Control"); cc != "no-cache" {
		t.Errorf("cache-control = %q, want no-cache (lockstep with the app shell)", cc)
	}
	if body := rec.Body.String(); !strings.Contains(body, "function renderCommitHead") {
		t.Error("body does not contain the extracted render functions")
	}
}

func TestAppRenderJSHandler_ServesGzipWhenAccepted(t *testing.T) {
	handler := AppRenderJSHandler()
	req := httptest.NewRequest(http.MethodGet, "/app-render.js", nil)
	req.Header.Set("Accept-Encoding", "gzip")
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if enc := rec.Header().Get("Content-Encoding"); enc != "gzip" {
		t.Fatalf("Content-Encoding = %q, want gzip", enc)
	}
	gr, err := gzip.NewReader(rec.Body)
	if err != nil {
		t.Fatalf("gzip.NewReader: %v", err)
	}
	got, err := io.ReadAll(gr)
	if err != nil {
		t.Fatalf("reading gzip body: %v", err)
	}
	if !strings.Contains(string(got), "function renderCommitHead") {
		t.Error("gzip body does not contain the extracted render functions")
	}
}

func TestAppCSSHandler_ServesCSS(t *testing.T) {
	handler := AppCSSHandler()
	req := httptest.NewRequest(http.MethodGet, "/app.css", nil)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); !strings.Contains(ct, "text/css") {
		t.Errorf("content type = %q, want text/css", ct)
	}
	if cc := rec.Header().Get("Cache-Control"); cc != "no-cache" {
		t.Errorf("cache-control = %q, want no-cache (lockstep with the app shell)", cc)
	}
	if body := rec.Body.String(); !strings.Contains(body, "@font-face") {
		t.Error("body does not contain the extracted stylesheet")
	}
}

func TestAppHelpersHandler_ServesGzipWhenAccepted(t *testing.T) {
	handler := AppHelpersHandler()
	req := httptest.NewRequest(http.MethodGet, "/app-helpers.js", nil)
	req.Header.Set("Accept-Encoding", "gzip")
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if enc := rec.Header().Get("Content-Encoding"); enc != "gzip" {
		t.Fatalf("Content-Encoding = %q, want gzip", enc)
	}
	if cc := rec.Header().Get("Cache-Control"); cc != "no-cache" {
		t.Errorf("Cache-Control = %q, want no-cache even when gzipped", cc)
	}
	gr, err := gzip.NewReader(rec.Body)
	if err != nil {
		t.Fatalf("gzip.NewReader: %v", err)
	}
	got, err := io.ReadAll(gr)
	if err != nil {
		t.Fatalf("reading gzip body: %v", err)
	}
	if !strings.Contains(string(got), "function formatDuration") {
		t.Error("decompressed body does not contain the extracted helper functions")
	}
}

func TestAppCSSHandler_ServesGzipWhenAccepted(t *testing.T) {
	handler := AppCSSHandler()
	req := httptest.NewRequest(http.MethodGet, "/app.css", nil)
	req.Header.Set("Accept-Encoding", "gzip")
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if enc := rec.Header().Get("Content-Encoding"); enc != "gzip" {
		t.Fatalf("Content-Encoding = %q, want gzip", enc)
	}
	if cc := rec.Header().Get("Cache-Control"); cc != "no-cache" {
		t.Errorf("Cache-Control = %q, want no-cache even when gzipped", cc)
	}
	gr, err := gzip.NewReader(rec.Body)
	if err != nil {
		t.Fatalf("gzip.NewReader: %v", err)
	}
	got, err := io.ReadAll(gr)
	if err != nil {
		t.Fatalf("reading gzip body: %v", err)
	}
	if !strings.Contains(string(got), "@font-face") {
		t.Error("decompressed body does not contain the extracted stylesheet")
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

// The end-of-replay marker (T4.17) must arrive after the replayed history so
// the UI can tell "still replaying" from "caught up". An empty history still
// emits it, which is exactly the genuine-empty signal the loading skeleton
// waits on.
func TestSSEHandler_EmitsSyncedMarkerAfterHistory(t *testing.T) {
	broadcaster := events.NewBroadcaster()
	history := events.NewHistory("")
	history.Add(events.DeployEvent{ID: 1, Stack: "gitea", Status: events.StatusSuccess})

	handler := SSEHandler(broadcaster, nil, history, nil)
	req := httptest.NewRequest(http.MethodGet, "/api/events", nil)
	body := serveSSE(t, handler, req, nil).Body.String()

	if !strings.Contains(body, "event: synced\n") {
		t.Fatalf("expected a synced marker, got:\n%s", body)
	}
	// It must trail the history, not precede it — the UI settles on it as
	// "history done", so an early marker would defeat the skeleton.
	if strings.Index(body, `"stack":"gitea"`) > strings.Index(body, "event: synced\n") {
		t.Error("synced marker must come after the replayed history")
	}
}

// An empty history still emits the synced marker — the signal that flips the
// loading skeleton to the genuine-empty state (T4.17).
func TestSSEHandler_EmitsSyncedMarkerForEmptyHistory(t *testing.T) {
	handler := SSEHandler(events.NewBroadcaster(), nil, events.NewHistory(""), nil)
	req := httptest.NewRequest(http.MethodGet, "/api/events", nil)
	body := serveSSE(t, handler, req, nil).Body.String()

	if !strings.Contains(body, "event: synced\n") {
		t.Fatalf("expected a synced marker for empty history, got:\n%s", body)
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

// The stream carries the state baseline itself, so a client is never left
// painting from nothing.
func TestSSEHandler_SendsStateBaselineOnConnect(t *testing.T) {
	collect := func() []events.StateEvent {
		return []events.StateEvent{{Name: events.StateQueue, Data: map[string]any{"count": 2}}}
	}

	handler := SSEHandler(events.NewBroadcaster(), events.NewStateBroadcaster(), events.NewHistory(""), collect)
	req := httptest.NewRequest(http.MethodGet, "/api/events", nil)
	body := serveSSE(t, handler, req, nil).Body.String()

	if !strings.Contains(body, "event: "+events.StateQueue+"\n") {
		t.Fatalf("expected the %s baseline on connect, got:\n%s", events.StateQueue, body)
	}
	if !strings.Contains(body, `"count":2`) {
		t.Errorf("baseline payload missing, got:\n%s", body)
	}
}

// A state change published *while the baseline is being built* must still reach
// the client: the subscription is established before the baseline is collected,
// so such an event is delivered after it rather than falling into a gap where
// nobody is listening. Publishing from inside collect lands in exactly that
// window deterministically — with the two swapped, this event is lost forever,
// which is what left the UI's pending pill blank after a webhook.
func TestSSEHandler_StateChangeDuringBaselineIsNotLost(t *testing.T) {
	stateB := events.NewStateBroadcaster()
	collect := func() []events.StateEvent {
		stateB.Publish(events.StateEvent{Name: events.StateQueue, Data: map[string]any{"count": 9}})
		return []events.StateEvent{{Name: events.StateQueue, Data: map[string]any{"count": 0}}}
	}

	handler := SSEHandler(events.NewBroadcaster(), stateB, events.NewHistory(""), collect)
	req := httptest.NewRequest(http.MethodGet, "/api/events", nil)
	body := serveSSE(t, handler, req, nil).Body.String()

	if !strings.Contains(body, `"count":9`) {
		t.Fatalf("a state change published during the baseline was lost, got:\n%s", body)
	}
	// And it must arrive after the baseline, or the baseline would overwrite it.
	if strings.Index(body, `"count":9`) < strings.Index(body, `"count":0`) {
		t.Errorf("the newer state must follow the baseline, got:\n%s", body)
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
		ID:      1,
		Stack:   "gitea",
		Diffs:   map[string]string{"docker-compose.yml": "+new line"},
		Commits: []events.CommitInfo{{SHA: "def456", Subject: "feat: bump gitea", Author: "Jane Doe"}},
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
	if !strings.Contains(body, "feat: bump gitea") || !strings.Contains(body, "def456") {
		t.Errorf("expected commit metadata in response, got %s", body)
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

func TestPeerDiffsHandler_ProxiesAndForwardsStatus(t *testing.T) {
	// A stub peer serving one event's diff and 404 for anything else.
	peer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/events/42/diffs" {
			_, _ = io.WriteString(w, `{"diffs":{"x":"+a"}}`)
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer peer.Close()

	resolve := func(name, id string) (string, bool) {
		if name != "host-b" {
			return "", false
		}
		return peer.URL + "/api/events/" + id + "/diffs", true
	}
	mux := http.NewServeMux()
	mux.Handle("GET /api/peers/{name}/events/{id}/diffs", PeerDiffsHandler(resolve, peer.Client()))

	do := func(path string) *httptest.ResponseRecorder {
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
		return rec
	}

	// Known peer + present event → the peer's body is forwarded verbatim.
	if rec := do("/api/peers/host-b/events/42/diffs"); rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"diffs"`) {
		t.Errorf("proxy = %d %q, want 200 with the peer diff", rec.Code, rec.Body.String())
	}
	// An event the peer has evicted → its 404 is forwarded (UI falls back to the link).
	if rec := do("/api/peers/host-b/events/99/diffs"); rec.Code != http.StatusNotFound {
		t.Errorf("evicted event: got %d, want 404", rec.Code)
	}
	// Unknown peer → 404 from the handler itself, no fetch.
	if rec := do("/api/peers/nope/events/42/diffs"); rec.Code != http.StatusNotFound {
		t.Errorf("unknown peer: got %d, want 404", rec.Code)
	}
}

func TestPeerContainerLogsHandler_StreamsAndForwardsStatus(t *testing.T) {
	// A stub peer that streams two SSE frames for a known stack, echoing the tail
	// and service query so the test can assert they are forwarded; 404 for an
	// unknown stack.
	peer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/container-logs/gitea" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = fmt.Fprintf(w, "data: tail=%s services=%s\n\n",
			r.URL.Query().Get("tail"), r.URL.Query().Get("services"))
		_, _ = io.WriteString(w, "data: second line\n\n")
	}))
	defer peer.Close()

	resolve := func(name, stack string) (string, bool) {
		if name != "host-b" {
			return "", false
		}
		return peer.URL + "/api/container-logs/" + stack, true
	}
	mux := http.NewServeMux()
	mux.Handle("GET /api/peers/{name}/container-logs/{stack}", PeerContainerLogsHandler(resolve, peer.Client()))

	do := func(path string) *httptest.ResponseRecorder {
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
		return rec
	}

	// Known peer + stack → the SSE frames stream through as event-stream, and the
	// tail + service selection query is forwarded to the peer verbatim.
	rec := do("/api/peers/host-b/container-logs/gitea?tail=50&services=web,db")
	if rec.Code != http.StatusOK {
		t.Fatalf("proxy = %d, want 200", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); ct != "text/event-stream" {
		t.Errorf("Content-Type = %q, want text/event-stream", ct)
	}
	if body := rec.Body.String(); !strings.Contains(body, "data: tail=50 services=web,db") || !strings.Contains(body, "data: second line") {
		t.Errorf("proxied body = %q, want the peer's SSE frames with the forwarded tail + services", body)
	}
	// A stack the peer does not know → its 404 is forwarded (UI surfaces the error).
	if rec := do("/api/peers/host-b/container-logs/nope"); rec.Code != http.StatusNotFound {
		t.Errorf("unknown stack: got %d, want 404", rec.Code)
	}
	// Unknown peer → 404 from the handler itself, no fetch.
	if rec := do("/api/peers/nope/container-logs/gitea"); rec.Code != http.StatusNotFound {
		t.Errorf("unknown peer: got %d, want 404", rec.Code)
	}
}
