package ui

import (
	"context"
	"encoding/json"
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
	handler := IndexHandler(ThemeCatppuccin)
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
			handler := IndexHandler(theme)
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
			for _, placeholder := range []string{"__UI_THEME__", "__FAVICON_URI__", "__THEME_COLOR_DARK__", "__THEME_COLOR_LIGHT__"} {
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
		ID:    1,
		Stack: "gitea",
		Diffs: map[string]string{"docker-compose.yml": "+new line"},
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
