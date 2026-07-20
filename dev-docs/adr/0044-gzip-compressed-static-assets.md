# ADR-0044: Gzip-compressed app-shell assets, negotiated at request time

Status: accepted
Date: 2026-07-20

## Context

`index.html` (~155 KB), `app.css` (~96 KB) and `app-helpers.js` (~7.5 KB) are
served as-is from `internal/ui`'s embedded FS (ADR-0035) — no minification,
no compression. Combined they're the bulk of what a fresh page load
transfers.

ADR-0035 already settled the adjacent question — the UI ships with no build
step, no bundler, no external/CDN/npm dependency — but that decision was
about *authoring* the assets, not about how bytes reach the client. Nothing
about "no build step" implies "no compression at request time."

## Options considered

**Minify at build time.** Rejected: it reopens ADR-0035. Minification needs a
build step (a JS/CSS minifier as a build dependency, invoked from somewhere —
`go generate`, a Makefile target, or the Nix build), which is exactly the
tool the self-contained invariant was written to avoid. It would also make
`index.html`/`app.css` diffs harder to review, the opposite of why ADR-0035
split fonts and JS out in the first place.

**Compress at the reverse proxy.** Andy's own deployments sit behind Traefik,
which has a built-in compress middleware — zero code change for that specific
setup. Rejected as the *only* answer: skipper-cd is meant to be usable
without a reverse proxy in front (`docs/getting-started.md`'s bare `docker
run`/direct-port path), so any user not fronting it with a compressing proxy
would get the uncompressed bytes regardless.

**Gzip, negotiated per request, computed once at handler construction
(chosen).** Each of the three handlers already builds its payload once, at
`http.Handler` construction time (`IndexHandler` bakes in the theme,
`AppCSSHandler`/`AppHelpersHandler` just read the embedded file) — the
constructed `[]byte` never changes for the life of the process. Gzipping that
payload once, alongside the plain copy, and choosing between them per request
based on `Accept-Encoding` costs nothing at request time and needs nothing
beyond `compress/gzip` from the standard library.

Brotli was not considered further: it typically beats gzip by only a few
percent at this content size and isn't in the standard library, so it would
add a dependency for a marginal gain — gzip alone already cuts these three
assets by 63–80%.

## Decision

`internal/ui/compress.go` adds `staticAsset`, a small type wrapping a
`contentType` plus the plain and gzip-encoded forms of a byte payload.
`newStaticAsset` gzips once; `ServeHTTP` sets `Vary: Accept-Encoding` and
serves the gzip body with `Content-Encoding: gzip` when the request's
`Accept-Encoding` lists gzip, otherwise the plain body.

`IndexHandler`, `AppCSSHandler` and `AppHelpersHandler` build a `staticAsset`
instead of writing their `[]byte` directly. Handlers that set their own
`Cache-Control` (the latter two: `no-cache`, to stay in lockstep with the
shell they support) still do so — `staticAsset` only owns `Content-Type`,
`Vary` and the encoding choice.

`FontsHandler` and `IconsHandler` are untouched: woff2 and PNG are already
compressed binary formats, so gzipping them again buys nothing and would
just spend CPU. `ManifestHandler` and `ServiceWorkerHandler` are untouched
too — at under 4 KB each, gzip's own framing overhead eats most of the
saving.

## Consequences

- Wire size for a cold, gzip-capable load drops sharply: `index.html`
  158,810 → ~40,900 bytes, `app.css` 98,370 → ~20,480 bytes, `app-helpers.js`
  7,631 → ~2,870 bytes (measured with `gzip -c`; the shipped Go
  `compress/gzip` output is the same format, byte count may vary slightly by
  a few dozen bytes with the default compression level).
- A client that sends no `Accept-Encoding: gzip` (or none at all) still gets
  the exact same plain bytes as before — no behavior change for that path.
- `Vary: Accept-Encoding` is set on all three responses so an intermediary
  cache (or the browser's own cache) never serves the wrong representation
  to a client that doesn't support gzip.
- One new file, `internal/ui/compress.go`, with its own test file; no new
  dependency (`compress/gzip` is standard library) and no build-step change,
  keeping ADR-0035's invariant intact.
