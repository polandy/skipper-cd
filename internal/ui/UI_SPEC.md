# skipper-cd Web UI — Specification

Single-page application served at `/` when `ui_enabled: true`. No external JS dependencies. Real-time deployment events via SSE (`/api/events`).

---

## Design

Nautical-industrial dark theme. Key colour tokens:

| Token | Value | Meaning |
|---|---|---|
| `--amber` | `#f59e0b` | Active deploy, brand accent |
| `--teal` | `#2dd4bf` | Success, connected |
| `--red` | `#f87171` | Failed, error |
| `--slate` | `#64748b` | Skipped |

Fonts: **DM Sans** (UI) + **JetBrains Mono** (timestamps, stack names, badges). Background: deep navy (`#060a14`) with subtle grid overlay and amber radial glow at top centre.

---

## Layout

Sticky frosted-glass header (56 px) + centred main (max 1040 px).

**Header — left:** compass/helm SVG icon, `skipper-cd` wordmark (amber `-cd`), `LIVE` pill.

**Header — right:**
- **Deploy indicator** — shows active stack name(s) or `idle`; amber pulsing dot when deploying.
- **Skip filter toggle** — hides/shows skipped rows. Default: active (hidden). State persisted in `localStorage` key `hideSkipped`.
- **Time mode toggle** — switches Time column between relative (`Xs ago`) and absolute (`toLocaleString()`). Default: inactive (relative). State persisted in `localStorage` key `timeMode`. Tooltip always shows the other format.
- **Connection indicator** — `connecting` (amber pulse) → `connected` (teal) → `reconnecting` (red).

---

## Deploy table

5-column grid (`110px 1fr 110px 80px 100px`): **Time · Stack · Status · Duration · Files**

Rows are prepended (newest first) with a slide-in animation. Time cells show relative or absolute time depending on the header toggle. Relative times refresh every 30 s; tooltip always shows the other format.

### Status badges

| Status | Colour | Notes |
|---|---|---|
| `deploying` | amber | Animated spinner dot |
| `success` | teal | |
| `failed` | red | Error panel expanded below row |
| `skipped` | slate | 35 % opacity row; hidden when filter active |

### Expandable panels

- **Files pill** — shown when `changed_files` is non-empty. Click toggles a list of file paths below the row.
- **Error detail** — shown for `failed` events with an `error` field. Monospace, red-tinted, `pre-wrap`.

---

## Event lifecycle (SSE)

On connect, history is replayed as `deploy` events, then live events stream in.

| Transition | Behaviour |
|---|---|
| `deploying` | New row prepended, tracked in memory. |
| `success` / `failed` (deploying row exists) | Existing row mutated in-place; error panel appended if needed. |
| `success` / `failed` (no existing row) | New row created directly. |
| `skipped` | New row created; hidden immediately if filter is active. |

---

## Responsive (≤ 700 px)

Column header hidden. Rows collapse to 2×2 grid (stack + badge on row 1, time + duration on row 2). Files column hidden.
