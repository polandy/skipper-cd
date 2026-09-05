package ui

import (
	"embed"
	"io/fs"
	"net/http"
	"strings"
)

// Static asset handlers: the embedded UI shell and everything it loads.
// Each is served from the embedded FS through staticAsset, so a gzip-capable
// client gets the pre-compressed body (compress.go).

//go:embed static/index.html static/app.css static/app.js static/app-state.js static/app-panels.js static/app-hosts.js static/app-roster.js static/app-autosync.js static/app-logs.js static/app-clog.js static/app-render.js static/app-helpers.js static/manifest.webmanifest static/sw.js static/icons static/fonts
var staticFS embed.FS

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
	return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
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
	return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
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

// AppRenderJSHandler serves GET /app-render.js — the pure HTML-string render
// layer extracted from the app script so a node --test unit layer can exercise
// it without a browser. The app shell loads it after app-helpers.js (whose
// functions it calls as globals) and before app.js. Served no-cache so it
// stays in lockstep with the shell; the service worker caches it in the app
// shell for offline use. Served via staticAsset, so a gzip-capable client gets
// the pre-compressed body (see compress.go).
func AppRenderJSHandler() http.Handler {
	data, err := staticFS.ReadFile("static/app-render.js")
	if err != nil {
		panic(err) // staticFS embeds static/app-render.js at compile time, so this cannot fail.
	}
	asset := newStaticAsset("text/javascript; charset=utf-8", data)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-cache")
		asset.ServeHTTP(w, r)
	})
}

// AppStateJSHandler serves GET /app-state.js — the App namespace and the
// shared snapshot store the view files read (ADR-0035 amendment). The app
// shell loads it after app-helpers.js and app-render.js (whose resolve*
// helpers it binds) and before app.js. Served no-cache so it stays in
// lockstep with the shell; the service worker caches it in the app shell for
// offline use. Served via staticAsset, so a gzip-capable client gets the
// pre-compressed body (see compress.go).
func AppStateJSHandler() http.Handler {
	data, err := staticFS.ReadFile("static/app-state.js")
	if err != nil {
		panic(err) // staticFS embeds static/app-state.js at compile time, so this cannot fail.
	}
	asset := newStaticAsset("text/javascript; charset=utf-8", data)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-cache")
		asset.ServeHTTP(w, r)
	})
}

// AppPanelsJSHandler serves GET /app-panels.js — the per-stack detail panels
// and the row affordances that open them, shared by every view (ADR-0035
// amendment). The app shell loads it after app-state.js and before the view
// files. Served no-cache so it stays in lockstep with the shell; the service
// worker caches it in the app shell for offline use. Served via staticAsset,
// so a gzip-capable client gets the pre-compressed body (see compress.go).
func AppPanelsJSHandler() http.Handler {
	data, err := staticFS.ReadFile("static/app-panels.js")
	if err != nil {
		panic(err) // staticFS embeds static/app-panels.js at compile time, so this cannot fail.
	}
	asset := newStaticAsset("text/javascript; charset=utf-8", data)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-cache")
		asset.ServeHTTP(w, r)
	})
}

// AppHostsJSHandler serves GET /app-hosts.js — the multi-host surface
// (ADR-0048), cut out of the app script by view (ADR-0035 amendment). The app
// shell loads it after app-panels.js and before app.js. Served no-cache so it
// stays in lockstep with the shell; the service worker caches it in the app
// shell for offline use. Served via staticAsset, so a gzip-capable client gets
// the pre-compressed body (see compress.go).
func AppHostsJSHandler() http.Handler {
	data, err := staticFS.ReadFile("static/app-hosts.js")
	if err != nil {
		panic(err) // staticFS embeds static/app-hosts.js at compile time, so this cannot fail.
	}
	asset := newStaticAsset("text/javascript; charset=utf-8", data)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-cache")
		asset.ServeHTTP(w, r)
	})
}

// AppRosterJSHandler serves GET /app-roster.js — the Stacks view, cut out of
// the app script by view (ADR-0035 amendment). The app shell loads it after
// app-hosts.js and before app.js. Served no-cache so it
// stays in lockstep with the shell; the service worker caches it in the app
// shell for offline use. Served via staticAsset, so a gzip-capable client gets
// the pre-compressed body (see compress.go).
func AppRosterJSHandler() http.Handler {
	data, err := staticFS.ReadFile("static/app-roster.js")
	if err != nil {
		panic(err) // staticFS embeds static/app-roster.js at compile time, so this cannot fail.
	}
	asset := newStaticAsset("text/javascript; charset=utf-8", data)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-cache")
		asset.ServeHTTP(w, r)
	})
}

// AppAutosyncJSHandler serves GET /app-autosync.js — the autosync control and
// drawer (ADR-0016), cut out of the app script by view (ADR-0035 amendment).
// The app shell loads it after app-state.js and before app.js. Served no-cache
// so it stays in lockstep with the shell; the service worker caches it in the
// app shell for offline use. Served via staticAsset, so a gzip-capable client
// gets the pre-compressed body (see compress.go).
func AppAutosyncJSHandler() http.Handler {
	data, err := staticFS.ReadFile("static/app-autosync.js")
	if err != nil {
		panic(err) // staticFS embeds static/app-autosync.js at compile time, so this cannot fail.
	}
	asset := newStaticAsset("text/javascript; charset=utf-8", data)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-cache")
		asset.ServeHTTP(w, r)
	})
}

// AppLogsJSHandler serves GET /app-logs.js — the Logs view, cut out of the app
// script by view (ADR-0035 amendment). The app shell loads it after
// app-state.js and before app.js. Served no-cache so it stays in lockstep with
// the shell; the service worker caches it in the app shell for offline use.
// Served via staticAsset, so a gzip-capable client gets the pre-compressed
// body (see compress.go).
func AppLogsJSHandler() http.Handler {
	data, err := staticFS.ReadFile("static/app-logs.js")
	if err != nil {
		panic(err) // staticFS embeds static/app-logs.js at compile time, so this cannot fail.
	}
	asset := newStaticAsset("text/javascript; charset=utf-8", data)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-cache")
		asset.ServeHTTP(w, r)
	})
}

// AppClogJSHandler serves GET /app-clog.js — the container-logs panel
// (ADR-0037), the first view file cut out of the app script (ADR-0035
// amendment). The app shell loads it after app-state.js and before app.js.
// Served no-cache so it stays in lockstep with the shell; the service worker
// caches it in the app shell for offline use. Served via staticAsset, so a
// gzip-capable client gets the pre-compressed body (see compress.go).
func AppClogJSHandler() http.Handler {
	data, err := staticFS.ReadFile("static/app-clog.js")
	if err != nil {
		panic(err) // staticFS embeds static/app-clog.js at compile time, so this cannot fail.
	}
	asset := newStaticAsset("text/javascript; charset=utf-8", data)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-cache")
		asset.ServeHTTP(w, r)
	})
}

// AppJSHandler serves GET /app.js — the main app script, extracted from
// index.html's inline <script> (ADR-0035 amendment) so it is lintable and
// formattable as a plain-script file. The app shell loads it last: it calls
// the helpers and renderers as globals and reads the store app-state.js set up. Served
// no-cache so it stays in lockstep with the shell that loads it; the service
// worker caches it in the app shell for offline use. Served via staticAsset,
// so a gzip-capable client gets the pre-compressed body (see compress.go).
func AppJSHandler() http.Handler {
	data, err := staticFS.ReadFile("static/app.js")
	if err != nil {
		panic(err) // staticFS embeds static/app.js at compile time, so this cannot fail.
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
