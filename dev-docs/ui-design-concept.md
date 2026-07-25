# UI design concept — shared row / table / panel language

The web UI has three top-level views (**Deploys · Stacks · Logs**, header
switcher). Deploys and Stacks are both **tables of stacks**, and they
deliberately share one visual language so switching between them feels like the
same surface with different data. This document is the reference for that
language; keep new views consistent with it. The authoritative element list
(test-ids, states) lives in [`internal/ui/UI_SPEC.md`](../internal/ui/UI_SPEC.md).

## Principle

**One look and feel across every view.** All pages share the same design
concept, achieved by **reusing the same components with the same CSS** rather
than building parallel ones. A new feature that needs a "badge" uses the badge
pattern (health pill / hooks badge), a "panel below a row" uses the variant-A
[bound panel](#expand--bound-panels), a "log" uses the container-logs panel
component, a touch hint uses the `.tap-tip` bubble on any control marked
`data-taptip` (deliberately opt-in, not every titled element — see
`setupTapTips` in `internal/ui/static/index.html`), and colours come from the
shared status/health [tokens](#tokens). Reach for an existing component and its
CSS before adding anything new; duplicated-but-slightly-different styling is the
thing this document exists to prevent. (Example: deploy hooks (ADR-0038) added a
badge, a bound panel, and a log *without* new component CSS — the badge mirrors
the health pill, the panel is variant A, and the log is the container-logs panel
in a second mode.)

**Budget every addition.** Reuse keeps the *look* consistent; it does not keep
the surface small. The row already carries a status badge, a health pill, a
files pill, a jump button, a logs icon, an app link, and — per view — an
overflow menu or inline actions; the expand card already stacks three panels.
Each was justified on its own, and the sum is what an operator has to read. So
a change that adds a badge, chip, panel, or row control answers two questions
in its PR:

1. **Which existing element could carry this instead?** A new state on a badge
   that is already there beats a second badge beside it; a line in an existing
   panel beats a fourth panel.
2. **What comes off, or why does nothing?** "Nothing, because X" is a fine
   answer — an unanswered question is not.

This is a design-review prompt, not a veto: the goal is that density grows on
purpose rather than one justified feature at a time.

One row = one stack. The two table views (Deploys · Stacks) differ only in
**what** the columns say, not in **how** rows, columns, and expansions look. The
single sanctioned difference: Deploys rows carry a **status-coloured left bar**
(it is an event log), the Stacks roster does not (status is a badge, since it is
inventory). Everything else — frame, hover, columns, the expand card — is
identical, and where practical the *same CSS rules* are shared rather than
duplicated.

## Rows

- **Borderless**, `var(--radius)` corners, transparent background; a subtle
  `--overlay2` wash on `:hover`. Rows sit in a flex column with a **2 px** gap.
  (`.event-row`, `.roster-row`.)
- **Left status bar** (Deploys only): a 3 px `::before` bar coloured by status
  (success/failed/queued/…). The roster omits it.
- **Padding** `12px 20px` desktop, `10px 14px` mobile.

## Columns

- Header row + data rows share **one fixed-track grid** so columns line up
  across rows (auto/`1fr`-only grids self-size per row and look ragged). Each
  view has a header (`.event-list-header`, `.roster-list-header`) with the same
  type treatment (10 px uppercase, muted, bottom rule).
  - Deploys: `Time · Stack · Status · Duration · Files`.
  - Stacks: `Stack · Status · Last deploy · Commit`.
- **Mobile (≤ 700 px)**: the header hides and each row collapses to a **2×2**
  grid (identity + status on top, a time + one metric below); the least
  important column (Files / Commit-less metrics) is dropped, never truncated
  off-screen.

## Expand → bound panels

Clicking a row (or a per-row affordance) inserts one or more **bound panels**
directly below it. The open row and its trailing panels read as **one card**:

- **Continuous left bar**: the open row and every bound panel carry the same
  3 px inset bar — the row's **status colour** in Deploys, the neutral
  **accent** for the history/containers panels (they are not a single status).
- **Seam**: the panel sets `margin-top: -2px` (closing the inter-row gap) and
  squares its **top** corners; the row squares its **bottom** corners. The bar
  runs unbroken from row into panel.
- **Closed card**: a bound panel keeps a **right + bottom border** and a
  **rounded bottom**, so the card is visibly closed on the right — identical for
  every panel type (`.diff-panel`, `.files-list`, `.health-panel`,
  `.audit-panel`, `.heal-panel`).
- **Stacking**: when panels stack (containers → history, or diff → error
  detail), every panel *except the last* squares its bottom (`…:has(+ …)`), so
  only the final card rounds off.

### Panel types

| Panel | Opened by | Shows |
|---|---|---|
| diff / files | row / files pill | changed files + git diff of the deploy |
| health (containers) | health pill (Deploys) · row (Stacks) | the stack's containers: name · state · health |
| audit (history) | clock button (Deploys) · row (Stacks) | past terminal deploy outcomes |
| heal | healed row | self-heal attempt detail |
| error-detail | failed/rolled-back row | the error message, full width |

In the **Stacks** roster, expanding a row stacks the **containers** panel
(current state) above the **history** panel (past deploys) as one accent card —
the same two panels the Deploys view opens separately.

## Time

Relative by default (`Xs ago`). A **shared time-mode toggle** lives in each
view's options popover (one mode, flipping it re-renders both views). Hovering a
relative time reveals the absolute value via `title` — a **mouse** affordance;
touch has no hover, so the toggle is the touch path.

## Tokens

Icons resolve from the stack name (`/api/icons/<name>`, monogram fallback).
Status badges (`badgeHTML`) and health dots use the shared status/health colour
variables. No view invents its own palette.
