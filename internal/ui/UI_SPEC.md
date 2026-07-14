# skipper-cd Web UI — Specification

Single-page application served at `/` when `ui_enabled: true`. No external JS dependencies. Real-time deployment events via SSE (`/api/events`); real-time log lines via SSE (`/api/logs`). The `/api/events` stream also carries `autosync` and `queue` events that drive the autosync controls and the queue drawer live (see [Autosync](#autosync)).

---

## Design

Nautical-industrial theme, built on a **configurable palette** (`ui_theme` in `skipper.yml`, see [`docs/configuration.md`](../../docs/configuration.md#web-ui-theme)): **Catppuccin** (Mocha dark / Latte light) is the default, plus four built-ins — **Nord**, **Solarized**, **Gruvbox**, **Rosé Pine** — each with its own dark and light variant. The theme is a per-deployment choice, baked server-side into `<html data-theme="…">` at serve time (`ui.IndexHandler`); it never flashes because it is present in the initial HTML, not applied by JS. When enabled (`ui_theme_switcher`, off by default), the header **theme picker** additionally lets a single browser override it locally without touching the deployment (see [Theme override](#theme-override)). Dark/light is a separate, per-browser axis: the header toggle only flips a `.light` class on top of whichever theme is active (`localStorage` key `colorScheme` = `light`/`dark`; a pre-paint inline script in `<head>` applies the class before first render — dark is the stylesheet default per theme, so a missing preference can never flash). `color-scheme` follows the active variant.

One semantic token layer consumes the palette; all tints, borders and glows are derived from it via `color-mix()`, so no component rule names a raw colour:

| Token | Meaning |
|---|---|
| `--accent` | Active deploy, brand accent, active toggles, connecting |
| `--success` | Success, connected |
| `--danger` | Failed, errors, reconnecting, diff deletions |
| `--rollback` | Rolled back |
| `--skip` | DEBUG log level (aliases the muted text tier) |
| `--queued` | Queued/deferred deploy, pending count, autosync paused (shares its colour with `--hunk`, distinct semantic) |
| `--diff-add` | Diff additions |
| `--hunk` | Diff hunk headers, WARN log level |

Each theme maps these tokens to its own raw colours (`--raw-accent`, `--raw-success`, …) under `:root[data-theme="<name>"]` / `:root[data-theme="<name>"].light`; adding a theme is a self-contained CSS block, nothing else in the page references a theme name. Background depth: `crust` (sunken — log pane, diff/files panels) → `mantle` (page) → `base` (header glass, cards) → `surface0` (raised — tags, toggle tracks). Text: `text` / `subtext0` / `overlay1` (primary / secondary / muted).

Fonts: **DM Sans** (UI) + **JetBrains Mono** (timestamps, stack names, badges). Both are **self-hosted and embedded** in `index.html` as `@font-face` rules with the `woff2` (latin subset, weights 400/500/600) inlined as `data:` URIs — there is no external Google Fonts request, so the page is fully self-contained, works offline, and renders deterministically for visual snapshots (see [`docs/e2e-tests.md`](../../docs/e2e-tests.md) §5). Background: mantle with subtle grid overlay and peach radial glow at top centre.

---

## Layout

Sticky frosted-glass header (56 px) + centred main (max 1040 px).

**Header — left:** skipper-cd container-ship logo (inline SVG, 32 px — a hull with wave carrying three container boxes: one in `--accent`, one in `--success`, one outlined; hull, outline and wave follow `--text-primary` via `currentColor`, so the logo tracks the theme toggle) and, stacked beside it, a text column: the `skipper-cd` wordmark (accent `-cd`) over a small muted **version label** showing the deployed build identity (`v<semver> · <commit>`, or the branch name for feature builds; local builds without ldflags show `dev`). The label is fetched once on load from `GET /api/version`, left empty until it resolves, and carries a `title` tooltip with the full string. It is a shrinkable flex item (`min-width: 0`) that shows in full whenever the header has room and only clips with an ellipsis when space genuinely runs out — no fixed character cap. The favicon is the same ship as an SVG data URI, its colours drawn from the configured theme, with a `prefers-color-scheme` media query switching between its light and dark variant (favicons cannot follow the in-page toggle).

**Header — right.** The controls are **icon/glyph-only** on every viewport (no text labels or switch tracks in the header row). Each glyph follows the theme via `currentColor`, is muted by default, and only takes on colour to signal a real state. Pointer users get the native `title` tooltip on hover; since touch never shows `title` tooltips, a tap on any header control flashes its label in a small **tap-reveal bubble** (the control's action still fires). In order:

- **Deploy indicator** — an **anchor** glyph (muted) at rest, swapping to a **ship** (accent, pulsing) with the active stack name(s) beside it while deploying. The names sit to the *left* of the glyph so the icon (and everything right of it) stays put as text appears/disappears. `idle` / `deploying <stacks>` is mirrored into `title` + `aria-label`. Visible in both views.
- **View toggle** — segmented control of two icons: a rows/table glyph (`deploys`) and a terminal glyph (`logs`). Default: `deploys`. State persisted in `localStorage` key `activeView`. The **active** button carries a small `▾` and opens the [View-options popover](#view-options-popover); the other button switches views.
- **Autosync control** — a single sync-arrows glyph showing global autosync state; **muted by default**, turning `--queued` (amber) when paused and `--accent` while its drawer is open. When deploys are queued it also shows an amber **pending count** pill (hidden at zero). Not a `localStorage` preference — it reflects server state from the `autosync`/`queue` SSE events. Click (or `Enter`/`Space`) toggles the [Autosync drawer](#autosync). Visible in both views.
- **Theme picker** — a palette glyph over a transparent native `<select>` of the five built-in palettes (see [Theme override](#theme-override)); clicking opens the native option list. **Opt-in**: present only when `ui_theme_switcher: true` (see [`docs/configuration.md`](../../docs/configuration.md#web-ui-theme)); off by default, so the deployed theme is fixed. Desktop only (hidden ≤ 700 px). Visible in both views.
- **Theme toggle** — a moon (dark, default) / sun (light) glyph switching between the configured theme's dark and light variant. State persisted in `localStorage` key `colorScheme` (`dark` / `light`). Visible in both views.
- **Connection indicator** — a **chain-link** glyph: a closed link in `--success` when `connected`, a closed link pulsing `--accent` while `connecting`, and a **broken link** pulsing `--danger` while `reconnecting`. State is on `data-state`. Bound to `/api/events`; the log stream has no own indicator.

### View-options popover

The view-specific toggles live in a small popover anchored under the view toggle (styled like the [Autosync drawer](#autosync)), **not** in the header row — so switching views never makes a header control appear or disappear. It is opened by clicking the already-active view button (the `▾` hint), and dismisses on outside-click or `Esc`; it and the Autosync drawer are mutually exclusive. Inside, each option is a full row (glyph + label + switch track). Contents by active view:

- **Deploys** → **Time mode** — switches the Time column between relative (`Xs ago`) and absolute (`toLocaleString()`). Default: inactive (relative). `localStorage` key `timeMode`.
- **Logs** → **Sort** — reverses log order; default inactive (newest first), active flips to oldest-first (terminal semantics). `localStorage` key `logSort` (`desc` / `asc`); flipping resets the visible window to one page. And **Auto-scroll (Follow)** — auto-scrolls the log pane to the newest line on every append; default active. `localStorage` key `followLogs`.

### Theme override

The theme picker is **opt-in**: it renders only when `ui_theme_switcher: true`. By default the flag is off — the picker is absent, the pre-paint script ignores any saved `themeOverride`, and the deployed `ui_theme` is fixed and cannot be switched from the browser. The switcher exists mainly to try palettes out. When enabled, it lets a **browser** show a different palette than the one `ui_theme` configures for the environment, without touching the deployment. This is purely a local, client-side preference:

- **Gating** — the flag is baked into `<html data-theme-switcher="on|off">` at serve time (`ui.IndexHandler`). The pre-paint script and the picker JS both read it: with the switcher off the `<select>` is hidden (CSS) and no override or mismatch notice is applied, so a `themeOverride` left in `localStorage` from a time the flag was on lies dormant until it is re-enabled.

- **Applying** — selecting a theme sets `document.documentElement`'s `data-theme` attribute immediately (every theme's CSS is always present in the stylesheet, so there is no reload and no flash). `<html>` carries a second, immutable attribute, `data-server-theme`, holding the value the environment is actually configured with — set once at serve time by `ui.IndexHandler` and never touched by JS. It is the reference the mismatch notice compares against.
- **Persisting** — a non-default choice is saved to `localStorage` key `themeOverride`. Picking the theme that matches `data-server-theme` again removes the override, so the page goes back to following whatever the environment is configured for (including across a `ui_theme` change on a later deploy). A pre-paint inline script in `<head>` re-applies a saved override before first render, the same no-flash approach the colour-scheme toggle uses.
- **Mismatch notice** — whenever `themeOverride` is set and differs from `data-server-theme` (which, by the rule above, is whenever an override exists at all), a dismissible notice appears under the header: *"Showing `<override theme>` in this browser — this environment is configured for `<server theme>`."* It auto-hides after 6 seconds, or immediately on clicking its close button; the check re-runs (and the notice reappears if still applicable) on every page load and every theme-picker change, but never nags mid-session beyond that.
- **Scope** — the override never reaches the server: there is no endpoint to change `ui_theme` from the UI. It only ever affects the browser that set it.

---

## Deploy table

5-column grid (`160px 1fr 110px 80px 100px`): **Time · Stack · Status · Duration · Files**

Rows are prepended (newest first) with a slide-in animation. Time cells show relative or absolute time depending on the header toggle. Relative times refresh every 30 s; tooltip always shows the other format.

### Stack icons

The Stack cell carries a small icon chip (18 px, fixed box, `object-fit: contain`) left of the name for recognition. The image is served same-origin from `GET /api/icons/<stack>` (no CSP concern); on any load error the chip swaps to a **monogram** — the stack's first letter on an accent-tinted chip — via the `<img>` `error` handler, so a broken image never shows. Icons are resolved server-side (repo `icon.svg`/`icon.png` override → configured `icon:` slug → auto-match on the stack name → 404 → monogram) and cached on the host; see the README "Service Icons" section.

**Refreshing icons.** A manual refresh clears the server-side icon cache (`POST /api/icons/refresh`) and reloads every visible icon with a cache-busting query param, so renamed stacks and newly published icons appear. It is deliberately **not a header control** — rarely needed, so it stays off the header — and is triggered by the **`i`** hotkey (global, ignored only while typing in an input). The endpoint is also reachable directly (e.g. `curl -X POST …/api/icons/refresh`) for ops use.

### Status badges

| Status | Colour | Notes |
|---|---|---|
| `deploying` | `--accent` (peach) | Animated spinner dot |
| `success` | `--success` (teal) | |
| `failed` | `--danger` (red) | Error panel expanded below row |
| `rolled_back` | `--rollback` (maroon) | Deploy failed but old containers restored; error panel shows details |
| `queued` | `--queued` (yellow) | Deploy deferred — autosync paused, change waiting; tinted row with amber left bar and a `paused: <global\|stack>` tag on the stack cell. See [Autosync](#autosync). |

### Expandable panels

- **Files pill** — shown when `changed_files` is non-empty. Click inserts a full-width panel as a sibling element directly below the row (same pattern as the error detail panel). Clicking again removes the panel.
- **Diff panel** — when `has_diffs` is `true` on the event, clicking the files pill fetches diffs from `GET /api/events/{id}/diffs` and renders a syntax-colored diff panel instead of the plain file list. Each file is a collapsible section (single-file diffs default to expanded). Diff lines are colored: additions (green), deletions (red), hunk headers (yellow), metadata (muted). Diffs are cached client-side after the first fetch.
- **Error detail** — shown for `failed` events with an `error` field. Monospace, red-tinted, `pre-wrap`.

---

## Event lifecycle (SSE)

On connect, history is replayed as `deploy` events, then live events stream in.

| Transition | Behaviour |
|---|---|
| `deploying` | New row prepended, tracked in memory. |
| `success` / `failed` / `rolled_back` (deploying row exists) | Existing row mutated in-place; error panel appended if needed. |
| `success` / `failed` / `rolled_back` (no existing row) | New row created directly. |
| `skipped` | Dropped — never rendered (an unchanged stack carries no signal). |
| `queued` | Row created with the `queued` badge and a `paused:` tag, **keyed by stack**: a further `queued` for the same stack (another push while paused) replaces it rather than stacking a duplicate. It is removed when the stack next deploys (a `deploying` event supersedes it) or when the stack leaves the pending set in a `queue` snapshot (resumed then found unchanged). Like a deploy, a `queued` event carries `has_diffs`, so the paused row expands the pending diff. |

---

## Autosync

Controls whether detected changes deploy automatically, per stack and globally. Paused stacks queue their changes and deploy them when sync resumes. Behaviour, semantics and the API contract are specified in [`docs/autosync.md`](../../docs/autosync.md); this section covers only the UI surface.

**Header control** — the [Autosync control](#header--right) shows global state and, when deploys are waiting, an amber **pending count** pill (hidden at zero). It is the drawer opener and the "how many are queued" indicator. It reflects server state (the `autosync`/`queue` SSE events), never `localStorage`.

**Autosync drawer** — an on-demand panel anchored under the header, opened by the control. Default hidden; closes on outside-click or `Esc`. Updated in real time from the `autosync` and `queue` SSE events, and painted from `GET /api/autosync` + `GET /api/queue` when opened. Contents, top to bottom:

- **Global autosync** — a switch (the header control mirrors its state). Toggling posts `POST /api/autosync {scope:"global", enabled}`.
- **Queued (N) · drains in this order** — the pending stacks in **deploy order** (`_nixos` first, then `skipper.yml` order), each row: position number, stack name, a `reason` chip (`global` / `stack`), changed-file count, and how long it has waited. Empty/hidden when nothing is queued.
- **All stacks** — one switch per managed stack with its current state, preceded by a **filter field** (case-insensitive substring match on stack name; a clear button appears when non-empty; `Esc` clears the field first, then closes the drawer). A "No stack matches …" state shows when the filter excludes everything. Toggling posts `POST /api/autosync {scope:"stack", stack, enabled}`. The switch reflects `effective`, so it stays correct across a toggle **while a filter is applied** (the toggle re-renders the list but preserves the query and the matched subset).

**Per-stack switch is an exception, not a pin.** A per-stack UI override is held only while it differs from what the stack would inherit; toggling a stack back to its inherited value clears the override (the "return to global" gesture) and toggling the global switch collapses any per-stack override that now matches the baseline. So the global switch behaves as a true master and a UI pause does not survive a global off→on cycle. Full semantics: [`docs/autosync.md`](../../docs/autosync.md#override-collapse) / [ADR-0019](../../docs/adr/0019-autosync-ui-overrides-collapse-to-inherit.md).

**Enable triggers a drain; disable does not.** Enabling sync (global or a stack) triggers a deploy run that drains the queue; disabling only updates state. Switches use the same track/thumb geometry as the header `.filter-toggle`.

---

## Autosync & queue API

`GET /api/autosync`, `POST /api/autosync`, `GET /api/queue`, and the `autosync` / `queue` SSE events on `/api/events` are specified in [`docs/autosync.md`](../../docs/autosync.md). The `POST` shares the trust level of the other endpoints (unauthenticated at the process; edge auth in front).

---

## Log view

Full-width monospace pane (bounded height, own scrollbar) showing all skipper-cd log output. Default order is **newest-first** (newest line at the top); the header **Sort toggle** flips to oldest-first (newest at the bottom, terminal semantics). Each line: muted timestamp, level badge, optional stack prefix, message, then dim `key=value` attrs.

Timestamps show the time of day; lines from another day get a date prefix, and the full `toLocaleString()` timestamp is always in the tooltip.

Level colours: `ERROR` → red, `WARN` → yellow, `DEBUG` → muted, `INFO` → secondary text. Lines with a `stack` attr (the deploy lifecycle: `deploying stack`, `deploy complete`, failures) render it as an accent-coloured `[gitea]`-style prefix and omit it from the trailing attrs, so what was deployed when is scannable. Child-process lines (attrs contain `cmd` and `stream`) render a muted `[docker]`-style command prefix instead of a level badge and the message in primary text; child output carries no stack attribution (known limitation — the runner does not know which stack it runs for).

`deploy complete` lines carry the deploy event's ID as an `event_id` attr (logged only when an event sink is configured, i.e. `ui_enabled`). The log view renders it as a **diff pill** instead of a plain attr: clicking fetches the deploy's diff from `GET /api/events/{id}/diffs` and inserts the same collapsible diff panel used by the deploy table directly below the line (click again to close; a notice appears when no diff was recorded, e.g. the event fell out of the bounded history).

Received lines are held in a bounded client-side buffer (2000 entries, oldest dropped on overflow) and the pane renders a sliding window of it. The window starts at **500 lines** (the newest 500) and grows by **500** each time the user scrolls to the older edge (the bottom when newest-first, the top when oldest-first); a scroll that reveals older lines preserves the reading position. Live lines are added incrementally at the newest edge, and the rendered window is trimmed back to its size from the older edge — trimmed entries stay in the buffer, so scrolling back reveals them again. Toggling sort rebuilds the pane and resets the window to one page. The `EventSource` for `/api/logs` is created lazily on first activation of the view and kept open afterwards; while the view is hidden, lines are buffered and the pane is rebuilt from the buffer on re-activation.

---

## Log API

`GET /api/logs` — SSE stream of captured log lines, event name `log`, payload:

```json
{"id":1720012345001,"time":"2026-07-10T12:00:00Z","level":"INFO","msg":"deploying stack","attrs":{"stack":"gitea"}}
```

On connect the in-memory backlog (bounded ring, 1000 entries, no persistence across restarts) is replayed — filtered by `Last-Event-ID` on reconnect — then live entries stream in. Entry IDs are seeded from the process start time so they stay monotonic across restarts. Slow consumers have lines dropped rather than blocking the logger. Same trust level as `/api/events` (unauthenticated); child-process output (`docker compose`, `git`, `nixos-rebuild`) is included — see ADR-0013.

---

## Diff API

`GET /api/events/{id}/diffs` — returns `{"diffs": {"filepath": "diff content", ...}}` or `{"diffs": null}`. Returns 404 for unknown event IDs.

## Version API

`GET /api/version` — returns the build identity `{"version": "<semver>|dev", "branch": "<name>", "commit": "<short-sha>"}`. Fields are injected at build time via `-ldflags "-X main.version=… -X main.commit=… -X main.branch=…"`:

- `version` — semver from `.release-please-manifest.json` (`dev` for local builds without ldflags).
- `commit` — short git SHA. Injected by the Nix flake (`self.shortRev`) and by Docker/CI; for a local `go build` it is recovered from the Go build info (`-dirty` suffix for an uncommitted tree). May be empty.
- `branch` — git branch name. Only CI/Docker builds know it; the Nix flake and plain local builds leave it empty.

The header label is painted once on load: a **feature-branch** build (branch set and ≠ `main`) shows `branch · commit`; otherwise it shows `v<version> · commit` (or `dev · commit`). The same `version-commit` string is baked into the service worker's cache name (`/sw.js`) so two feature-branch builds that share a release semver still bust the app-shell cache.

Diffs are stored in `deploy-history.yaml` alongside events but are **not** included in SSE payloads (only `has_diffs: true` is sent). This keeps the real-time stream lightweight. Large diffs are truncated at 10 KB per file and 50 KB total per event.

---

## Progressive Web App (PWA)

The UI is an **installable PWA** (full spec: [`docs/pwa.md`](../../docs/pwa.md); decision: [ADR-0018](../../docs/adr/0018-pwa-installable-ui.md)). It is an enhancement layer — the page behaves exactly as before in a normal browser tab.

- **Manifest** — `GET /manifest.webmanifest` (`<link rel="manifest">` in `<head>`): `name` `skipper-cd`, `short_name` `skipper`, `display` `standalone`, `start_url`/`scope` `/`, `theme_color` `#1e1e2e` (Mocha base), `background_color` `#181825` (Mocha mantle) for the splash. Icons at 192 and 512 px (`purpose: any`) plus a 512 px `maskable` variant, all rendered from the ship logo and served under `/icons/`.
- **App identity** — the ship logo becomes the app icon (including a maskable variant so Android crops it into the system shape without clipping). Served as PNG because iOS ignores SVG and manifest icons for the home screen; an `apple-touch-icon` link and `apple-mobile-web-app-*` metas cover iOS.
- **Theme colour** — two `<meta name="theme-color" media="(prefers-color-scheme: …)">` tags (dark `#1e1e2e`, light Latte `#eff1f5`) let the OS window/status-bar colour follow the **OS** light/dark preference. Like the favicon, this cannot follow the in-page Mocha/Latte toggle (a platform limitation).
- **Service worker** — `GET /sw.js` (registered after `load`, failure-tolerant). Caches only the **app shell** (`/`, manifest, icons) and serves it **network-first with a cache fallback**, so a reachable server always wins and a just-deployed UI is picked up promptly. **Invariant: live traffic is never cached** — the worker bypasses `/api/*` (incl. the SSE streams `/api/events`, `/api/logs`), `/metrics`, and `/webhook` without `respondWith`, so streaming is untouched. The cache name carries the build version, so a new release changes the served `sw.js` bytes, the browser adopts the new worker, and stale caches are dropped.
- **Secure context** — installability requires HTTPS (or `localhost`); the usual TLS reverse proxy satisfies this. Over plain HTTP the UI still works but is not installable. No new configuration — the PWA is active whenever `ui_enabled: true`.

---

## Test hooks (`data-testid`)

The E2E UI suite ([`docs/e2e-tests.md`](../../docs/e2e-tests.md)) selects **only** on
`data-testid` — never on `id`, text, or CSS class — so refactoring markup or
styling never breaks the tests. These attributes are a public contract of the
UI; keep them stable when editing `index.html`. Dynamic rows also carry data
attributes (`data-stack`, `data-status`, `data-level`, `data-state`) the tests
assert on.

| `data-testid` | Element | Notes |
|---|---|---|
| `brand-name` | Header `skipper-cd` wordmark | Stacked over the version label; hidden ≤ 700 px (logo alone carries the brand) |
| `brand-version` | Header version label | `v<semver> · <commit>` / branch; `dev` local; empty until `/api/version` resolves; full string in `title`; shown in portrait ≤ 700 px |
| `view-toggle` | Deploys/Logs segmented icon control | Active button (`.active`) opens the view-options popover |
| `deploy-indicator` | Deploy indicator (anchor/ship glyph) | `idle`/`deploying <stacks>` in `title` + `aria-label` |
| `autosync-btn` | Header autosync control (drawer opener) | `data-global` = `true`/`false` (global autosync state) |
| `pending-pill` | Amber pending-count pill | Hidden at zero |
| `view-options` | View-options popover (opened from the active view button) | `.open` when shown; holds `time-mode` / `log-sort` / `follow-logs` |
| `time-mode`, `log-sort`, `follow-logs` | View-specific toggle buttons | Inside `view-options`; hidden until the popover opens |
| `theme-toggle` | Header theme (dark/light) toggle | Glyph-only; moon in dark, sun in light |
| `theme-select` | Header theme picker (`<select>`) | Present only when `ui_theme_switcher` is enabled; transparent over a palette glyph. See [Theme override](#theme-override) |
| `theme-notice` | Theme mismatch notice | Shown when `themeOverride` differs from `data-server-theme` |
| `theme-notice-close` | Theme mismatch notice's dismiss button | |
| `conn-indicator` | Connection indicator (chain-link glyph) | `data-state` = `connecting`/`connected`/`reconnecting` |
| `empty-state` | Awaiting-events placeholder | |
| `deploys-table` | The deploys view container (header + rows) | Snapshot anchor (UA1) |
| `deploy-row` | A deploy table row | `data-stack`, `data-status` |
| `status-badge` | Status badge inside a row | |
| `stack-icon` | Icon chip in the stack cell | |
| `time-cell`, `duration-cell` | Time / duration cells | Masked in snapshots (dynamic) |
| `files-pill` | Files pill on a row | |
| `files-panel` | Expanded plain file-list panel | |
| `diff-panel` | Expanded diff panel | |
| `error-panel` | Error detail panel under a failed row | |
| `log-line` | A log line | `data-level` = level (or `cmd` for child output) |
| `level-badge` | Log level badge | |
| `stack-prefix` | `[stack]` prefix on a deploy log line | |
| `cmd-prefix` | `[cmd]` prefix on child-process output | |
| `diff-pill` | Diff pill on a `deploy complete` log line | |
| `autosync-drawer` | The autosync drawer | |
| `global-switch` | Global autosync switch | |
| `stack-item` | A row in the "All stacks" list | `data-stack` |
| `stack-switch` | Per-stack switch in "All stacks" | `data-stack`; only in the all-stacks list |
| `queue-item` | A row in the queued list | `data-stack` |
| `wait-cell` | Wait-time text on a queue item | Masked in snapshots (dynamic) |
| `stack-filter` | Stack filter input | |
| `stack-filter-clear` | Filter clear button | |

---

## Responsive (≤ 700 px)

**Header — compact single row.** The header is already glyph-only on every viewport (see [Header — right](#layout)), so little changes ≤ 700 px beyond tightening the row to 48 px, which **must never scroll horizontally**. The brand is the ship logo plus, **in portrait**, the version label (it clips with an ellipsis and can shrink so the row still never scrolls; the full string stays in its `title` tooltip). The `skipper-cd` wordmark is hidden; the version label is also dropped in the tighter landscape orientation. Control-specific changes:

- **Deploy indicator** — the deploying stack names are dropped (kept in `title`/`aria-label`); only the anchor/ship glyph shows.
- **Autosync control** — unchanged (already icon-only); keeps its glyph and pending-count pill.
- **View toggle / theme toggle / connection** — unchanged; already glyph-only.
- **View-options popover** — unchanged; still opened from the active view button.
- **Theme picker** — hidden entirely; overriding the palette is rarely needed on a phone. The configured theme (and any override already saved from a previous desktop visit) still applies.

On touch there are no hover tooltips, so each control's label is reachable via the **tap-reveal bubble** (a tap flashes the `title`; the action still fires). The `.status-area` gap tightens to 10 px and the header padding to 12 px so the row fits a 360 px viewport without overflow.

**Deploy table.** Column header hidden. Rows collapse to a 2×2 grid (stack + badge on row 1, time + duration on row 2). Files column hidden.

Since the Files pill is not visible on mobile, tapping anywhere on a row (that has `changed_files`) triggers the files/diff panel instead. Rows with changed files get `cursor: pointer` on mobile. The toggle behaviour (tap again to close) is identical to the desktop pill behaviour.

The Autosync drawer spans the full width below the header.
