# skipper-cd Web UI — Specification

## Overview

The web UI is a single-page application served by skipper-cd when `ui_enabled: true` is set in the configuration. It provides a real-time view of all deployment events via Server-Sent Events (SSE). The UI is implemented as a single self-contained HTML file (`internal/ui/static/index.html`) with no external JavaScript dependencies.

---

## Visual Design

### Theme

Nautical-industrial dark theme.

| Token | Value | Usage |
|---|---|---|
| `--bg-deep` | `#060a14` | Page background |
| `--bg-base` | `#0b1120` | Header backdrop |
| `--bg-surface` | `#111827` | Surface elements |
| `--bg-raised` | `#1a2436` | Raised elements, toggle track |
| `--border` | `rgba(148,163,184,0.08)` | Default borders |
| `--border-lit` | `rgba(148,163,184,0.15)` | Active/hover borders |
| `--text-primary` | `#e2e8f0` | Main text |
| `--text-secondary` | `#94a3b8` | Secondary text |
| `--text-muted` | `#475569` | Muted/disabled text |
| `--amber` | `#f59e0b` | Active deployments, brand accent |
| `--teal` | `#2dd4bf` | Successful deployments, connected state |
| `--red` | `#f87171` | Failed deployments, error state |
| `--slate` | `#64748b` | Skipped deployments |

### Background

- Full-viewport subtle grid pattern overlay (48×48 px, `rgba(148,163,184,0.02)`)
- Radial amber glow at the top centre of the page

### Typography

- **Display:** `DM Sans` (400, 500, 600, 700) — body text, labels, badges
- **Mono:** `JetBrains Mono` → `SF Mono` → `Cascadia Code` → monospace — timestamps, stack names, code, status indicators
- Both fonts loaded from Google Fonts

---

## Layout

```
┌─────────────────────────────────────────────────────────┐
│  HEADER  (sticky, 56 px, frosted-glass backdrop)        │
│  [brand]                  [deploy] [filter] [conn]      │
├─────────────────────────────────────────────────────────┤
│  MAIN  (max-width 1040 px, centred, 32 px top margin)   │
│                                                         │
│  Empty state  —OR—  Deploy table                        │
└─────────────────────────────────────────────────────────┘
```

---

## Header

### Brand (left side)

| Element | Details |
|---|---|
| Icon | Inline SVG compass/helm (32×32 px). Amber north spoke + arrow, dim grey spokes on other three axes. |
| Name | `skipper-cd` in monospace; `-cd` part rendered in `--amber`. |
| Tag | `LIVE` pill badge in muted style (`--bg-raised`, uppercase, 10 px mono). |

### Status area (right side, left-to-right order)

#### Deploy indicator

- Label: stack name(s) being deployed, or `idle` when none active.
- Dot: 7 px circle.
  - Idle: `--text-muted`, no animation.
  - Active: `--amber` with pulsing `breathe` animation (opacity 1 → 0.4, 2 s).

#### Skip filter toggle

- A `<button>` styled as a toggle switch.
- Label text: `hide skipped`.
- **Default state:** active (skipped rows hidden).
- State persisted in `localStorage` key `hideSkipped` (string `"true"` / `"false"`).
- Visual states:

| State | Toggle track | Thumb | Label colour |
|---|---|---|---|
| Active (hiding) | `--amber-dim` bg, amber border | `--amber`, translated right 12 px | `--amber` |
| Inactive (showing) | `--bg-raised` bg, `--border-lit` border | `--text-muted`, left position | `--text-muted` |

#### Connection indicator

- Dot: 7 px circle + label text.

| State | Dot colour | Animation | Label |
|---|---|---|---|
| Connecting | `--amber` | `breathe` pulse | `connecting` |
| Connected | `--teal` | none | `connected` |
| Reconnecting | `--red` | none | `reconnecting` |

---

## Main content

### Empty state

Shown when no deployment events have been received yet.

- Centred compass SVG icon (64×64 px, 30 % opacity).
- Text: `Awaiting deployment events...` in mono, muted colour.

### Deploy table

Shown once the first event arrives. Replaces the empty state permanently.

#### Column header

5-column grid: `Time | Stack | Status | Duration | Files`

```
grid-template-columns: 110px 1fr 110px 80px 100px
```

Header row is hidden on viewports ≤ 700 px.

#### Event rows

Each deployment event occupies one row in the same 5-column grid.

| Column | Class | Content |
|---|---|---|
| Time | `.cell-time` | Relative time (see below). Full ISO timestamp in `title` attribute. |
| Stack | `.cell-stack` | Stack name, monospace, truncated with ellipsis. |
| Status | `.status-cell` | Status badge (see below). |
| Duration | `.cell-duration` | Human-readable duration, or `—` if unavailable. |
| Files | `.col-files` | Expandable file count pill, or `—` if no files. |

New rows are prepended (newest at top) with a slide-in animation (`rowEnter`, 0.4 s).

##### Relative time format

| Age | Display |
|---|---|
| < 5 s | `just now` |
| < 60 s | `Xs ago` |
| < 1 h | `Xm ago` |
| < 24 h | `Xh ago` |
| ≥ 24 h | `Xd ago` |

Relative times are refreshed every 30 seconds via `setInterval`.

##### Duration format

| Value | Display |
|---|---|
| 0 / missing | `—` |
| < 60 s | `Xs` |
| ≥ 60 s | `Xm Ys` |

---

## Status badges

Displayed inside `.status-cell`. All badges: mono font, 11 px, uppercase, 5 px border-radius.

| Status | Class | Colour | Background | Special |
|---|---|---|---|---|
| `deploying` | `.badge-deploying` | `--amber` | `--amber-dim` | Animated spinner dot prepended |
| `success` | `.badge-success` | `--teal` | `--teal-dim` | — |
| `failed` | `.badge-failed` | `--red` | `--red-dim` | — |
| `skipped` | `.badge-skipped` | `--slate` | `--slate-dim` | — |

---

## Row visual states

| Status | Row class | Background | Left border | Opacity |
|---|---|---|---|---|
| `deploying` | `.deploying-row` | `--amber-dim` | 3 px `--amber` with amber glow | — |
| `success` | `.success-row` | none | 3 px `--teal` (visible on hover only) | — |
| `failed` | `.failed-row` | `--red-dim` | 3 px `--red` | — |
| `skipped` | `.skipped` | none | none | 35 % (55 % on hover) |

---

## Expandable file list

When `changed_files` is non-empty, the Files cell shows a pill button:

- Folder SVG icon (12×12 px) + `N file(s)` label.
- Clicking toggles a `.files-list` panel below the row (full-column span).
- Each file path rendered as an inline `<span class="file-path">` chip.
- Expand/collapse animation: 0.2 s fade + slide.

---

## Error detail panel

When a `failed` event includes an `error` string, a full-width panel is inserted directly below the row:

- Red-tinted background (`rgba(248,113,113,0.04)`), red border.
- Monospace, 11 px, `pre-wrap` whitespace, 1.6 line-height.
- Inserted via `row.after(errorDetailEl)`.

---

## Real-time event handling

### Transport

SSE stream at `/api/events`. The browser auto-reconnects on connection loss.

### Event lifecycle

All events carry the `deploy` SSE event type and a JSON payload with at minimum:

```
{ id, stack, status, timestamp, duration_ms?, changed_files?, error? }
```

| Transition | Behaviour |
|---|---|
| `deploying` received | New row prepended, registered in `deployingRows[stack]`. Deploy indicator updates to show stack name. |
| `success` / `failed` received, row exists | Existing `deploying` row is mutated in-place (class, badge, duration, time). Error panel appended if `error` present. Row removed from `deployingRows`. |
| `success` / `failed` received, no existing row | New row created directly (history replay path). |
| `skipped` received | New row created; hidden immediately if skip filter is active. |

---

## Skip filter

- Controlled by the header toggle button (`#skip-filter`).
- When active: class `hide-skipped` is added to `#tbody`; CSS rule `display: none` hides all `.event-row.skipped` elements.
- State initialised from `localStorage.hideSkipped` on page load. Default: `true` (active).
- State written to `localStorage` on each toggle click.

---

## Responsive behaviour (≤ 700 px)

- Header padding reduced to 16 px; `LIVE` tag hidden.
- Column header row hidden.
- Event rows switch to a 2-column, 2-row grid:
  - Row 1: Stack name (left) / Status badge (right)
  - Row 2: Timestamp (left, 11 px) / Duration (right, aligned)
- Files column hidden.

---

## Backend API contract

| Endpoint | Method | Description |
|---|---|---|
| `/` | `GET` | Serves `index.html` |
| `/api/events` | `GET` | SSE stream, `Content-Type: text/event-stream`. On connect, replays history as `deploy` events, then streams live events. |
