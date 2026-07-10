# skipper-cd Web UI — Specification

Single-page application served at `/` when `ui_enabled: true`. No external JS dependencies. Real-time deployment events via SSE (`/api/events`); real-time log lines via SSE (`/api/logs`).

---

## Design

Nautical-industrial dark theme. Key colour tokens:

| Token | Value | Meaning |
|---|---|---|
| `--amber` | `#f59e0b` | Active deploy, brand accent |
| `--teal` | `#2dd4bf` | Success, connected |
| `--red` | `#f87171` | Failed, error |
| `--orange` | `#fb923c` | Rolled back |
| `--slate` | `#64748b` | Skipped |

Fonts: **DM Sans** (UI) + **JetBrains Mono** (timestamps, stack names, badges). Background: deep navy (`#060a14`) with subtle grid overlay and amber radial glow at top centre.

---

## Layout

Sticky frosted-glass header (56 px) + centred main (max 1040 px).

**Header — left:** skipper-cd ship logo (transparent PNG, embedded as base64 data URI, 32 px), `skipper-cd` wordmark (amber `-cd`), `LIVE` pill. Same logo used as favicon.

**Header — right:**
- **View toggle** — segmented `deploys | logs` control switching between the deploy table and the log view. Default: `deploys`. State persisted in `localStorage` key `activeView`.
- **Deploy indicator** — shows active stack name(s) or `idle`; amber pulsing dot when deploying. Visible in both views.
- **Skip filter toggle** — hides/shows skipped rows. Default: active (hidden). State persisted in `localStorage` key `hideSkipped`. Deploys view only.
- **Time mode toggle** — switches Time column between relative (`Xs ago`) and absolute (`toLocaleString()`). Default: inactive (relative). State persisted in `localStorage` key `timeMode`. Tooltip always shows the other format. Deploys view only.
- **Follow toggle** — auto-scrolls the log pane to the newest line on every append. Default: active. State persisted in `localStorage` key `followLogs`. Logs view only.
- **Connection indicator** — `connecting` (amber pulse) → `connected` (teal) → `reconnecting` (red). Bound to `/api/events`; the log stream has no own indicator.

---

## Deploy table

5-column grid (`160px 1fr 110px 80px 100px`): **Time · Stack · Status · Duration · Files**

Rows are prepended (newest first) with a slide-in animation. Time cells show relative or absolute time depending on the header toggle. Relative times refresh every 30 s; tooltip always shows the other format.

### Status badges

| Status | Colour | Notes |
|---|---|---|
| `deploying` | amber | Animated spinner dot |
| `success` | teal | |
| `failed` | red | Error panel expanded below row |
| `rolled_back` | orange | Deploy failed but old containers restored; error panel shows details |
| `skipped` | slate | 35 % opacity row; hidden when filter active |

### Expandable panels

- **Files pill** — shown when `changed_files` is non-empty. Click inserts a full-width panel as a sibling element directly below the row (same pattern as the error detail panel). Clicking again removes the panel.
- **Diff panel** — when `has_diffs` is `true` on the event, clicking the files pill fetches diffs from `GET /api/events/{id}/diffs` and renders a syntax-colored diff panel instead of the plain file list. Each file is a collapsible section (single-file diffs default to expanded). Diff lines are colored: additions (teal), deletions (red), hunk headers (amber), metadata (muted). Diffs are cached client-side after the first fetch.
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

Full-width monospace pane (bounded height, own scrollbar) showing all skipper-cd log output, newest line at the **bottom** (terminal semantics — unlike the deploy table's prepend). Each line: muted `toLocaleTimeString()` timestamp, level badge, message, then dim `key=value` attrs.

Level colours: `ERROR` → red, `WARN` → amber, `DEBUG` → slate, `INFO` → secondary text. Child-process lines (attrs contain `cmd` and `stream`) render a slate `[docker]`-style command prefix instead of a level badge and the message in primary text.

The rendered DOM is capped at 1000 lines; the oldest line is removed on overflow. The `EventSource` for `/api/logs` is created lazily on first activation of the view and kept open afterwards.

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

Column header hidden. Rows collapse to 2×2 grid (stack + badge on row 1, time + duration on row 2). Files column hidden.

Since the Files pill is not visible on mobile, tapping anywhere on a row (that has `changed_files`) triggers the files/diff panel instead. Rows with changed files get `cursor: pointer` on mobile. The toggle behaviour (tap again to close) is identical to the desktop pill behaviour.
