# UI design concept — shared row / table / panel language

Status: living reference — kept in sync with the UI.

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
`setupTapTips` in `internal/ui/static/app-chrome.js`), and colours come from the
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
single sanctioned difference is what the left gutter is keyed to: **deploy
status** in Deploys (it is an event log) and **health** in the Stacks roster
(status there is the badge, since it is inventory). Both follow the same rule —
the gutter marks the exception and stays empty for the normal case. Everything
else — sheet, hover, columns, the expand card — is identical, and where
practical the *same CSS rules* are shared rather than duplicated.

## Rows

- Rows live **on a sheet**, not on the page: the column header (`.sheet-head`)
  and the row list (`.sheet-body`) are one bordered `--r-sheet` surface, rows sit
  flush inside it with a hairline between them and no radius of their own — only
  the last child follows the sheet's bottom corners. A subtle `--overlay2` wash
  on `:hover`. (`.event-row`, `.roster-row`.)
- **Left status bar**: a 3 px `::before` running the row's **full height**,
  coloured by `data-status` in Deploys, so the table's left edge is a status
  gutter. **The gutter marks exceptions, not the norm**: `success` is silent at
  rest and only answers a hover, because it is over nine rows in ten and
  painting it left an unbroken green line for the two rows worth finding to
  compete with. Nothing is lost — the status column says it in words and an icon
  regardless, which is what lets the redundant channel drop the norm and keep
  the exception. `healed` stays drawn (it shares the success green but is rare,
  so green comes to mean "self-heal acted"). The roster follows the same rule on
  its own axis: it draws the bar only for a health state that needs it
  (unhealthy, stopped), since status there is the badge.
- **No row tint.** A status is said once by the bar and once by the badge; the
  full-row wash that used to say it a third time is gone. What still tints is the
  **error strip** under a failed row, which is the part that needs the alarm, and
  the row of a deploy running right now.
- **Padding** `12px 20px` desktop, `10px 14px` mobile.

## Columns

- Header row + data rows share **one fixed-track grid** so columns line up
  across rows (auto/`1fr`-only grids self-size per row and look ragged). Each
  view has a header (`.event-list-header`, `.roster-list-header`) with the same
  type treatment: the sheet's head strip (`.sheet-head`), 12 px sentence-case
  labels in the display face on a faintly sunken ground.
  - Deploys: `Time · Stack · Version · Status · Duration · Files`.
  - Stacks: `Stack · Version · Status · Last deploy · Commit`.
- **One version chip everywhere** (`.tag-delta`): both views render versions with
  the same component — a deploy's `old → new` change in Deploys, a stack's running
  version in Stacks, each line of the containers panel. Never a second chip built
  per surface.
- **Tablet (≤ 1000 px)**: drop columns, don't squeeze them all. Deploys drops
  **Duration** and **Files**, Stacks drops **Commit** (header labels included),
  so **Version keeps the row's first line** in front of the status and stays a
  column to scan down. A cell that still runs out shrinks and ellipsises inside
  its track — a row never prints one column over the next — and the stack name
  keeps a floor: below it the row's affordances wrap to a second line inside the
  cell rather than the name being squeezed away. A cell that no longer fits its track
  shrinks and ellipsises **inside** it — a row never prints one column over the
  next, which is what a column layout is for.
- **Mobile (≤ 700 px)**: the header hides and each row is **two lines** —
  identity + status on the first, time + **version** on the second, the status
  cell spanning both so its health pill sits level with the versions. The
  columns the tablet drops stay dropped: never truncated off-screen, always
  reachable in the row's panels.

## Expand → bound panels

Clicking a row (or a per-row affordance) inserts one or more **bound panels**
directly below it. The open row and its trailing panels read as **one card**:

- **Continuous left bar**: the open row and every bound panel carry the same
  3 px inset bar — the row's **status colour** in Deploys, the neutral
  **accent** for the history/containers panels (they are not a single status).
- **Part of the sheet, not a card on it**: a panel sits flush under its row
  inside the same border, drawing no frame and no radius of its own — the only
  edges it adds are that left bar and the hairline the list puts between every
  child. Panels stack the same way, so a row with three of them is one unbroken
  run down the sheet.
- **Closing the card**: whichever panel ends the run follows the sheet's bottom
  corners, through the one `.sheet-body > :last-child` rule rather than a
  per-panel `:has(+ …)` chain. That is what the negative margins and
  corner-squaring the gapped-row layout needed used to buy.

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

## Type roles

Two faces, split by what the text **is** — not by which surface it sits on:

- **DM Sans** (`--font-display`) for anything a person wrote or reads as a label:
  stack names, times and durations, column headers, status and health words,
  panel section headings, file counts.
- **JetBrains Mono** (`--font-mono`) for machine output only: image versions,
  commit SHAs, diffs, log lines, paths.

A stack name in mono made every row read as a terminal dump; the split is what
gives a row a first and a second tier.

## Frame budget

Border, fill and radius each say "this is a separate object", so they are spent
where that is true rather than on every token in a row:

- **A status is a word with an icon.** Only the states that want attention take a
  fill — `failed` and `rolled_back` a tinted pill, the two worst outcomes
  (`rolled_back_unhealthy`, `heal_exhausted`) a solid one. Settled states are the
  icon plus the word.
- **Health is a dot and a word**, filled only on hover, where it says "this
  opens".
- **File counts, durations and times are text.** They gain a surface on hover if
  they are a control.
- **The row's glyph cluster rests at 0.62 opacity** and comes up on hover, focus,
  or while its own surface is open — legible on touch, quiet while scanning.
- **Radius is a scale, not a value**: `--r-sheet` (12px) for a sheet or band,
  `--radius` (8px) for a control or popover, `--r-chip` (5px) for a chip inside a
  row, `999px` for a state token.

## Tokens

Icons resolve from the stack name (`/api/icons/<name>`, monogram fallback).
Status badges (`badgeHTML`) and health dots use the shared status/health colour
variables. No view invents its own palette.
