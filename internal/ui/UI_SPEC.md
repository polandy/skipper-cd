# skipper-cd Web UI — Specification

Single-page application served at `/` when `ui_enabled: true`. No external JS dependencies. Real-time deployment events via SSE (`/api/events`); real-time log lines via SSE (`/api/logs`).

---

## Design

Nautical-industrial theme on the **Catppuccin** palette: **Mocha** is the dark default, **Latte** the opt-in light theme (header toggle, `localStorage` key `theme` = `latte`; a pre-paint inline script in `<head>` applies the `latte` root class before first render — dark is the stylesheet default, so a missing preference can never flash). `color-scheme` follows the palette.

One semantic token layer consumes the palette; all tints, borders and glows are derived from it via `color-mix()`, so no component rule names a raw colour:

| Token | Palette colour | Meaning |
|---|---|---|
| `--accent` | peach | Active deploy, brand accent, active toggles, connecting |
| `--success` | teal | Success, connected |
| `--danger` | red | Failed, errors, reconnecting, diff deletions |
| `--rollback` | maroon | Rolled back |
| `--skip` | overlay1 | Skipped, DEBUG log level |
| `--diff-add` | green | Diff additions |
| `--hunk` | yellow | Diff hunk headers, WARN log level |

Background depth: `crust` (sunken — log pane, diff/files panels) → `mantle` (page) → `base` (header glass, cards) → `surface0` (raised — tags, toggle tracks). Text: `text` / `subtext0` / `overlay1` (primary / secondary / muted).

Fonts: **DM Sans** (UI) + **JetBrains Mono** (timestamps, stack names, badges). Background: mantle with subtle grid overlay and peach radial glow at top centre.

---

## Layout

Sticky frosted-glass header (56 px) + centred main (max 1040 px).

**Header — left:** skipper-cd container-ship logo (inline SVG, 32 px — a hull with wave carrying three container boxes: one in `--accent`, one in `--success`, one outlined; hull, outline and wave follow `--text-primary` via `currentColor`, so the logo tracks the theme toggle), `skipper-cd` wordmark (accent `-cd`), `LIVE` pill. The favicon is the same ship as an SVG data URI with a `prefers-color-scheme` media query (Latte colours by default, Mocha when the OS is dark — favicons cannot follow the in-page toggle).

**Header — right:**
- **View toggle** — segmented `deploys | logs` control switching between the deploy table and the log view. Default: `deploys`. State persisted in `localStorage` key `activeView`.
- **Deploy indicator** — shows active stack name(s) or `idle`; amber pulsing dot when deploying. Visible in both views.
- **Skip filter toggle** — hides/shows skipped rows. Default: active (hidden). State persisted in `localStorage` key `hideSkipped`. Deploys view only.
- **Time mode toggle** — switches Time column between relative (`Xs ago`) and absolute (`toLocaleString()`). Default: inactive (relative). State persisted in `localStorage` key `timeMode`. Tooltip always shows the other format. Deploys view only.
- **Icon refresh** — a refresh-glyph action button (not a toggle) that clears the server-side icon cache (`POST /api/icons/refresh`) and reloads every visible icon with a cache-busting query param, so renamed stacks and newly published icons appear. Also bound to the **`i`** hotkey (ignored while typing in an input). Brief spin animation on activation. Deploys view only.
- **Sort toggle** — reverses log order. Default: inactive (newest first, newest line at the top). Active flips to oldest-first (terminal semantics, newest at the bottom). State persisted in `localStorage` key `logSort` (`desc` / `asc`). Flipping resets the visible window to one page. Logs view only.
- **Follow toggle** — auto-scrolls the log pane to the newest line (the top when newest-first, the bottom when oldest-first) on every append. Default: active. State persisted in `localStorage` key `followLogs`. Logs view only.
- **Theme toggle** — switches between Mocha (dark, default) and Latte (light). State persisted in `localStorage` key `theme` (`latte` / `mocha`). Visible in both views.
- **Connection indicator** — `connecting` (accent pulse) → `connected` (success) → `reconnecting` (danger). Bound to `/api/events`; the log stream has no own indicator.

---

## Deploy table

5-column grid (`160px 1fr 110px 80px 100px`): **Time · Stack · Status · Duration · Files**

Rows are prepended (newest first) with a slide-in animation. Time cells show relative or absolute time depending on the header toggle. Relative times refresh every 30 s; tooltip always shows the other format.

### Stack icons

The Stack cell carries a small icon chip (18 px, fixed box, `object-fit: contain`) left of the name for recognition. The image is served same-origin from `GET /api/icons/<stack>` (no CSP concern); on any load error the chip swaps to a **monogram** — the stack's first letter on an accent-tinted chip — via the `<img>` `error` handler, so a broken image never shows. Icons are resolved server-side (repo `icon.svg`/`icon.png` override → configured `icon:` slug → auto-match on the stack name → 404 → monogram) and cached on the host; see the README "Service Icons" section. Reload via the header **Icon refresh** control or the `i` hotkey.

### Status badges

| Status | Colour | Notes |
|---|---|---|
| `deploying` | `--accent` (peach) | Animated spinner dot |
| `success` | `--success` (teal) | |
| `failed` | `--danger` (red) | Error panel expanded below row |
| `rolled_back` | `--rollback` (maroon) | Deploy failed but old containers restored; error panel shows details |
| `skipped` | `--skip` (overlay1) | 35 % opacity row; hidden when filter active |

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
| `skipped` | New row created; hidden immediately if filter is active. |

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

Diffs are stored in `deploy-history.yaml` alongside events but are **not** included in SSE payloads (only `has_diffs: true` is sent). This keeps the real-time stream lightweight. Large diffs are truncated at 10 KB per file and 50 KB total per event.

---

## Responsive (≤ 700 px)

Column header hidden. Rows collapse to 2×2 grid (stack + badge on row 1, time + duration on row 2). Files column hidden. Header toggles lose their text labels and render as bare switches (tooltips remain).

Since the Files pill is not visible on mobile, tapping anywhere on a row (that has `changed_files`) triggers the files/diff panel instead. Rows with changed files get `cursor: pointer` on mobile. The toggle behaviour (tap again to close) is identical to the desktop pill behaviour.
