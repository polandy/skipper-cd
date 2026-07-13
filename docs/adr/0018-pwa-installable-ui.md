# ADR-0018: Installable PWA web UI with app-shell caching

Status: accepted
Date: 2026-07-12

## Context

The web UI is a single embedded page (`internal/ui/static/index.html`, ~215 KB
with fonts inlined as `data:` URIs, no external JS), served by `ui.IndexHandler()`
from a `go:embed` filesystem. It opens as a normal browser tab and re-downloads
its whole shell on every visit; it cannot be installed as a standalone app.

We want it to be an **installable Progressive Web App**: operators install it onto
a home screen / app launcher, it opens in its own standalone window with the ship
icon, and it starts quickly from a local cache. The full feature is specified in
[`docs/pwa.md`](../pwa.md); the UI surface in
[`internal/ui/UI_SPEC.md`](https://github.com/polandy/skipper-cd/blob/main/internal/ui/UI_SPEC.md).

Three questions dominated: how much to cache (offline ambition), what the service
worker must never touch, and how a cached shell stays fresh across skipper's own
self-deploys.

## Decision

### Installable + app-shell cache, not full offline

We add a web app manifest, a service worker, and PNG app icons (rendered from the
existing ship SVG, including a maskable variant). The service worker caches only
the **app shell** — the page, its inlined fonts, the icons.

We deliberately do **not** build a full offline experience. skipper-cd is a
real-time deploy dashboard; its value is live state. Caching stale deploy events
or logs for offline viewing would be misleading, so offline the app opens and
shows its normal "reconnecting" state rather than replaying old data.

### Live traffic bypasses the service worker

The service worker's `fetch` handler **does not call `respondWith`** for `/api/*`
(including the SSE streams `/api/events` and `/api/logs`), `/metrics`, or
`/webhook`. Those requests take the native network path untouched. This keeps SSE
streaming intact — a service worker that buffered a `text/event-stream` response
would break the real-time UI. "Live data is never cached" is the invariant here.

### Shell freshness tied to the deployed version

Because skipper-cd deploys itself, a cached shell must never pin the UI to an old
build. Two mechanisms combine:

- The service worker serves the shell **network-first with a cache fallback**, so
  a reachable server always wins and a just-deployed UI is picked up promptly; the
  cache is only a fast/offline fallback.
- The service worker's cache name carries the build version (`__VERSION__`,
  replaced at serve time with the same `-ldflags` version the UI header shows). A
  new release changes the served `sw.js` bytes, so the browser detects a new
  worker, which claims clients and drops caches under the old name.

### Serving

The manifest, service worker, and icons are embedded in the `ui` package
(`go:embed static/*`) and served by new handlers alongside `IndexHandler`, wired
in `cmd/skipper/main.go` under the existing `ui_enabled` block. `sw.js` is served
`Cache-Control: no-cache` so worker updates always propagate.

## Consequences

- The UI is installable and starts fast; nothing changes for users who keep using
  a normal browser tab, and no new configuration is introduced (PWA behaviour is
  on whenever `ui_enabled: true`).
- Installability requires a **secure context** — HTTPS or `localhost`. The usual
  TLS reverse proxy satisfies this; over plain HTTP the site still works but is not
  installable. This is an operational prerequisite, documented, not enforced by
  the app.
- The icon PNGs are committed to the repository (the embed needs the bytes); the
  `pwa-icon.svg` source is committed alongside them so they can be re-rendered.
- The service worker must keep the `/api/*` bypass forever; a future API route that
  streams must not be accidentally cached.
