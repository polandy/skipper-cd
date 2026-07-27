# Feature Spec: Service Icons

Status: implemented (see `docs/configuration.md` §Service Icons)
Date: 2026-07-11

## Goal

Give every stack a recognizable icon in the web UI overview, so stacks are
identifiable at a glance instead of by name alone. Icons are resolved
automatically from a public icon set where possible, with a per-stack override,
and are cached locally so they are fetched at most once.

Non-goals: per-service (intra-stack) icons, icon editing/upload UI, theming of
the icons themselves.

## Resolution order (per stack)

Highest priority wins; first hit stops the search:

1. **Repo override** — an `icon.png` (or `icon.svg`) file in the stack's
   directory inside the clone: `<stacks_base_dir>/<name>/icon.png`. Served
   straight from the clone. No network, always available offline.
2. **Config slug override** — an optional `icon:` field on the stack in
   `skipper.yml` naming an icon-set slug (e.g. `icon: jellyfin` for a stack
   named `media`). Resolved against the icon set, then cached.
3. **Auto-match** — the stack name, normalized to a slug (lowercased, spaces →
   `-`), looked up in the icon set. Cached.
4. **Fallback** — no icon found or offline: the UI renders a monogram (first
   letter of the stack name on an accent-tinted chip). Purely client-side, so
   the UI never shows a broken image.

## Icon set

- Source: [dashboard-icons](https://github.com/homarr-labs/dashboard-icons),
  the de-facto homelab icon set.
- Base URL (the set **root**) is configurable (`icons.source_url`, default the
  dashboard-icons CDN) so it can be mirrored or swapped, and to keep tests
  offline.
- dashboard-icons stores each format in its own directory. Lookup tries
  `<source_url>/svg/<slug>.svg`, then `png`, then `webp` (first hit wins) —
  **SVG preferred** (small, scales crisp) but many icons exist only as PNG/WebP,
  so those are essential fallbacks. A 404 in every format means "no such icon"
  → fall through to the monogram. Any other status is a transient error (not
  cached, retried).

## Local cache

- On-disk directory on the skipper host, persistent across restarts.
  Configurable via `icons.cache_dir`; default `/var/lib/skipper/icons`
  (alongside the existing state directory).
- Stores both **positive** results (the fetched icon bytes, keyed by sanitized
  slug + extension) and **negative** results (a marker that a slug 404'd), so a
  missing icon is not re-fetched on every UI load.
- Slugs are sanitized before touching the filesystem (no path separators, no
  `..`) — both for cache filenames and when reading the repo override — to
  prevent path traversal.

## HTTP surface (only when `ui_enabled: true`)

- `GET /api/icons/{stack}` — resolves the stack per the order above and returns
  the image bytes with the right `Content-Type` and long-lived
  `Cache-Control`. Repo override and cache hits serve immediately. On a cache
  miss it fetches once (short timeout, bounded by the existing command-timeout
  budget), caches the result, and serves it; on fetch failure it returns 404 so
  the UI shows the monogram fallback.
- `POST /api/icons/refresh` — clears the cache (positive **and** negative
  entries) so renamed stacks and newly-published icons get picked up on the
  next load. Returns 204.

## UI integration

- Each stack row in the deploy table gets a small icon chip to the left of the
  stack name (`<img src="/api/icons/<stack>">` with an `onerror` swap to the
  monogram fallback — same-origin only, no CSP concern).
- `POST /api/icons/refresh` clears the cache; icons are re-fetched on the next
  load. It is an **ops endpoint** — no UI control and no keyboard hotkey (the
  `i` hotkey was removed so the deploys type-to-search filter can own printable
  keys; see `internal/ui/UI_SPEC.md`).
- Icon rendering is size-normalized (fixed box, `object-fit: contain`) so mixed
  SVG/PNG sources line up.

## Package layout

- New package `internal/icons` owns the cache, the resolution logic, and the
  fetch client. The fetcher sits behind a consumer-side interface (like
  `command.Runner` / `git.CommitReader`) so tests inject a fake and never hit
  the network.
- The HTTP handlers live in `internal/icons` and are wired into `webhookMux`
  under the existing `ui_enabled` block in `cmd/skipper/main.go`.
- The type owns its representation (cache dir, source URL, clone path) and
  exposes methods; no raw maps handed around (encapsulation principle).

## Config additions

```yaml
icons:
  cache_dir: /var/lib/skipper/icons          # optional, default shown
  source_url: https://.../dashboard-icons/svg # optional, default shown
stacks:
  - name: media
    icon: jellyfin                            # optional slug override
```

- `icons` section is optional; omitting it uses defaults and keeps auto-match +
  repo overrides working.
- `icon:` on a stack is optional.
- Validation: `cache_dir` writable check is deferred to first use (fail soft →
  fallback, never crash the deploy path).

## Change detection

No change needed, but documented as an invariant interaction: `icon.png` in a
stack directory is **not** a hashed input (only compose/env_files/vars_file/
watch_dirs/build Dockerfiles are). Adding or changing an icon must never
trigger a redeploy. The one footgun: if a user lists the stack directory itself
as a `watch_dir`, the icon would be hashed — call this out in the README icon
section.

## Failure & offline behavior

- Offline / source unreachable: auto-match fails → monogram fallback. Repo
  overrides and previously-cached icons keep working.
- A slow or hanging fetch is bounded by a short per-fetch timeout; the UI shows
  the monogram until the icon is cached, and picks it up on the next load /
  refresh.
- Nothing in the icon path can block or fail a deploy — it is UI-only.

## Testing

- `internal/icons`: table tests with `t.TempDir()` caches and a fake fetcher;
  assert resolution order, slug sanitization (path-traversal attempts),
  positive/negative caching, and cache clearing.
- Handler tests with `httptest`: repo override served, cache hit served, cache
  miss triggers one fetch, fetch failure → 404, refresh clears cache.
- No real network; the fake fetcher is the seam.

## Resolved decisions

1. Repo override supports both `icon.svg` and `icon.png` — checked svg first,
   then png.
2. Negative cache is **clear-only** (no TTL); it is cleared by the explicit
   refresh action. Simplest and matches the refresh button.
3. Refresh (`POST /api/icons/refresh`) stays **lazy** — it clears the cache;
   icons are re-fetched on the next view, not eagerly re-warmed.
4. Refresh is an ops-only endpoint — no UI control or keyboard hotkey (see
   `internal/ui/UI_SPEC.md`).
