# ADR-0035: UI assets served same-origin from the embedded FS

Status: accepted
Date: 2026-07-18

## Context

The web UI shipped as a single embedded file, `internal/ui/static/index.html`,
and the guiding rule ("the web UI is ONE embedded file") was taken literally:
CSS, JS, and even the fonts all lived inside that one file. The fonts dominated
it — six base64-inlined `woff2` faces (DM Sans + JetBrains Mono, weights
400/500/600) were ~143 KB of a ~320 KB file, i.e. ~45 % of it and ~90 % of its
non-JS bulk. Consequences:

- The file is unwieldy to read and edit; the font blobs bury the actual markup
  and dominate every diff's line count.
- Base64 inflates the bytes by ~33 % over the raw `woff2`, and an inline `data:`
  font cannot be cache-validated or shared the way a real URL can.
- There is no seam to serve *any* asset separately — which also blocks pulling
  the pure JS helpers out into a file a node test runner can import (the
  JS-unit-test-layer follow-up).

But the literal "one file" reading was never the actual goal. What the UI needs
to guarantee is that it stays **self-contained**: everything ships inside the Go
binary, is served same-origin, needs no build step, and makes no external / CDN
/ npm request (so it works offline and renders deterministically for the visual
snapshots). "One file" was a convenient proxy for that, not the requirement.

## Options considered

**0. Status quo** — keep every asset inline in `index.html`. Rejected: the file
stays unreadable, the base64 keeps dominating diffs, and there is no seam for the
JS-unit-test-layer follow-up.

**External fonts (Google Fonts / a CDN).** Rejected outright: it breaks the
self-contained guarantee — an external network request, an offline failure mode,
a third-party dependency, and font-load nondeterminism back in the snapshots.

**A. Keep self-contained, but allow more than one embedded same-origin asset
(chosen).** Extract the six faces to real `static/fonts/*.woff2` files, still
`//go:embed`-ed into the binary and served same-origin from a scoped
`FontsHandler` under `/fonts/`; reference them from `@font-face … url(/fonts/…)`,
`<link rel="preload">` them in the head, and add them to the service worker's
app-shell `SHELL`. Redefine the invariant accordingly (see Decision).

## Decision

Redefine the UI invariant from "**one embedded file**" to "**self-contained**":

> The UI ships entirely inside the binary and is served same-origin — no build
> step, no JS/CSS bundler, no external/CDN/npm dependency. It may comprise more
> than one embedded asset (the app-shell HTML plus same-origin static files like
> fonts and, later, extracted JS), each served from `internal/ui`'s embedded FS.

Concretely, this PR:

- moves the six `woff2` faces to `internal/ui/static/fonts/*.woff2`
  (`//go:embed static/…/fonts`);
- serves them via `FontsHandler` (route `GET /fonts/`), scoped to `static/fonts`
  with `StripPrefix` exactly like `IconsHandler` — it can never reach the
  app-shell files — with an explicit `font/woff2` content type (Go's mime table
  omits it) and an immutable long-`max-age` cache;
- replaces the inline `data:` `@font-face` sources with `url(/fonts/…)` and
  preloads all six faces so first paint is unchanged;
- adds the fonts to the service worker `SHELL` (extending the app shell of
  ADR-0018), so an installed/offline UI still renders its real typography.

This same relaxation unblocks the separate JS-unit-test-layer change, which
extracts the pure JS helpers into an embedded, same-origin JS file.

## Consequences

- `index.html` drops from ~320 KB to ~177 KB; diffs of the markup are readable
  again.
- Fonts are cacheable and shared across pages/loads; the raw `woff2` is ~33 %
  smaller on the wire than the base64 was.
- Still fully self-contained and offline: `go build` embeds every asset, nothing
  is fetched cross-origin, and preload + the SW cache keep font-load timing out
  of the visual snapshots (no external request was ever, or is now, made).
- One more route and handler to hold, mirroring the existing icons handler; a
  new font is added by dropping a `woff2` in `static/fonts` and referencing it.
- The literal "ONE embedded file" wording in `CLAUDE.md` and `internal/ui/UI_SPEC.md`
  is updated to the self-contained definition above.

## Amendment (2026-07-19): extract the stylesheet into `static/app.css`

`index.html` had grown to ~4700 lines, roughly half of it (~2200 lines) a single
`<style>` block — CSS diffs dominated the file's history the same way the font
blobs once did, and reviews of a markup or script change had to scroll past
unrelated styling.

Applying the relaxation this ADR already established (self-contained, not
literally one file), the CSS moves to `internal/ui/static/app.css`, embedded via
the same `//go:embed` directive and served same-origin by a new `AppCSSHandler`
(`GET /app.css`), mirroring `AppHelpersHandler`: no-cache, `text/css`, added to
the service worker's `SHELL` for offline use. `index.html` keeps a single
`<link rel="stylesheet" href="/app.css">` in the `<head>` in place of the two
inline `<style>` blocks (the small `@font-face` block and the main stylesheet);
nothing else about the app shell changes — no build step, still same-origin,
still fully offline-capable.

`index.html` drops to ~2440 lines (markup + the main app script); `app.css`
holds the ~2225 lines of CSS on its own, readable and diffable independent of
markup/script changes.

## Amendment (2026-07-28): extract the app script into `static/app.js`

The same pressure, one asset later: `index.html` had grown back to ~5000 lines,
~93% of it the single inline app `<script>` — and because Prettier/ESLint
exclude `index.html` (a formatting concession to the markup), the script was
also opted out of linting and unit tooling entirely.

Applying the established relaxation again, the app script moves byte-identical
to `internal/ui/static/app.js`, embedded via the same `//go:embed` directive
and served same-origin by a new `AppJSHandler` (`GET /app.js`), mirroring
`AppHelpersHandler`: no-cache, `text/javascript`, pre-gzipped, added to the
service worker's `SHELL` for offline use. `index.html` keeps a single
`<script src="/app.js"></script>` after the `app-helpers.js` tag (load order
matters: the app script calls the helpers as globals). The tiny pre-paint
theme script stays inline in `<head>` — it must run before first render.

`index.html` drops to ~385 lines of markup; `app.js` holds the ~4600-line app
script where ESLint and Prettier can reach it, opening the path to migrating
DOM-free logic into the unit-tested `app-helpers.js` incrementally.

## Amendment (2026-07-28): extract the render layer into `static/app-render.js`

First step of the incremental migration the previous amendment opened: the pure
HTML-string builders (`escapeHtml`/`escapeAttr`, `commitLinkHTML`,
`versionChipHTML`, `imageDeltaHTML`, `renderCommitHead`) move from `app.js`
into `internal/ui/static/app-render.js` — named for what it holds, not
"helpers". Same serving pattern as the other extractions: embedded, served by
`AppRenderJSHandler` (`GET /app-render.js`, no-cache, pre-gzipped), in the
service worker's `SHELL`, loaded between `app-helpers.js` (whose functions it
calls as globals) and `app.js`.

Two deliberate changes from the verbatim move: `escapeHtml` is reimplemented as
pure string replacement (the DOM-based original needed a browser; the escaped
set — `& < >` — is exactly what the DOM's text-node serialization escapes), and
`renderCommitHead` takes the forge base as a `repoBase` parameter instead of
reading the module-scope `repoWebURL`. That purity is what lets
`app-render.test.js` exercise the layer under `node --test`.
