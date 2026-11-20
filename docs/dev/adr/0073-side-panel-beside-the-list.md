# ADR-0073: the detail panel sits beside the list on a wide screen

Status: accepted
Date: 2026-11-20

## Context

The Deploys and Stacks views each open one detail panel at a time
(`App.detail`, PR 2 of the UI redesign plan). Until now that panel was always a
bound panel inserted directly under its row. On a wide screen that has two
costs. The rows below the open one are pushed off screen by a panel that is
often taller than the viewport, so comparing the open stack with its
neighbours means closing it again. And the panel inherits the list's width
(up to ~1400 px), which stretches a diff or a facts strip into lines too long
to read, while the space beside a short list stays empty.

## Decision

**At a viewport of 1200 px or wider the open detail moves into a side column
to the right of its list.** Each view's list and an `<aside class="detail-side">`
share a `.detail-layout` grid. While a detail is open and its row is visible,
the layout carries `side-open`. The list then narrows to a 470 px column and
the panel takes the rest. The panel is sticky under the header and scrolls on
its own.

- **The narrowed list keeps identity and status.** Deploys keep Stack and
  Result and drop Change and When. Stacks keep Stack, Health and the last
  deploy's badge, and drop Version and the time and commit. Everything dropped
  is in the open panel.
- **Between 701 and 1199 px nothing changes.** The panel stays bound under
  its row, as before. The phone sheet (≤ 700 px) is the amendment below.
- **One breakpoint, two places that know it.** The CSS media query lays the
  grid out, and `matchMedia('(min-width: 1200px)')` in `app-detail.js` decides
  where the nodes go. A `change` event re-places an open detail, so a resize
  moves it between side and inline without closing it.
- **The nodes move only when their place changes.** A refresh that keeps the
  panel where it is leaves focus and scroll alone.
- **The side column follows its row's visibility.** When a filter or a host
  toggle hides the open row, the side column hides with it, as the inline
  panel did.
- **The panel keeps its close button.** The mockup showed none in side mode.
  Without one, closing the detail means finding the row again, and there is no
  touch equivalent of Escape.
- **Nothing is shown in the side column while no detail is open.** The list
  then keeps its full width and all columns. A placeholder would cost every
  wide screen half the table to say "click a row".

## Alternatives considered

- **Bound panels at every width.** This is the old behaviour. It is rejected
  because the panel pushes the rest of the list away on exactly the screens
  that have room for both.
- **The side column from 1000 px.** At 1000–1199 px a 470 px list leaves the
  panel under 530 px, too narrow for the four-column facts strip and a diff.
  Narrowing the list further cuts the stack name below its 7-character floor.
- **A permanent side column.** This means an empty-state panel while nothing
  is selected. It is rejected for the cost stated above. It would also turn
  the Deploys log into a master-detail screen even for an operator who only
  scans it.
- **The detail as an overlay or drawer.** This covers the list it belongs to.
  The drawers are for global settings, not per-row data.

## Consequences

- In side mode the side column comes after its list in the DOM, so the
  keyboard reaches the panel by tabbing past the remaining rows. Escape and
  the close button still return the focus to the row's toggle.
- Code that looked for the open panel as its row's next sibling now asks
  `App.detail` (`openRow`), and the click and key delegation of each list also
  listens on its side column.
- The e2e suite runs at 1280 px by default, so masks that checked a panel's
  binding now assert where it sits (`detailHome`). Mask BF covers both
  placements and the resize between them.

## Amendment 2026-11-20: the phone sheet

At a viewport of 700 px or narrower the open detail is a **full-screen sheet
over the list** (plan §3.4).

- **The sheet is the same side column.** `place()` sends the nodes to the side
  column when either `matchMedia` matches (`min-width: 1200px` or
  `max-width: 700px`); only the CSS differs. At ≤ 700 px the column is
  `position: fixed; inset: 0` above the header, and the panel head stays
  sticky at its top, so close is always in reach.
- **The list never moves.** While the sheet is open the page is locked
  (`overflow: hidden` on the body of the view that holds it), so its scroll
  offset is untouched and close returns to the list exactly where it was. The
  sheet scrolls on its own, with `overscroll-behavior: contain`.
- **A resize re-places it** like the side column: crossing 700 px moves an
  open detail between sheet and inline without closing it.
- **Rejected: inline on a phone, as before.** A detail taller than the screen
  pushed the rest of the list away and its close button scrolled out of reach.
  **Rejected: a separate sheet element.** It would duplicate the side
  column's placement, filter and Escape handling for the same nodes.
