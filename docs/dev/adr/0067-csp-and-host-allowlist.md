# ADR-0067: A strict CSP, and a Host allowlist against DNS rebinding

Date: 2026-11-07

## Status

Accepted. Builds on [ADR-0035](0035-ui-assets-same-origin-from-embed.md).

## Context

skipper ships no authentication; the docs put the UI behind a reverse proxy.
Two gaps stay open behind one, and wide open without one:

- **No security headers.** The UI builds its HTML by string concatenation.
  The escaping is correct, but one missed call site (a `status` reaching
  `badgeHTML` unescaped from a peer was one) is a script injection with
  nothing behind it. The page could also be framed, which makes the autosync
  toggle and the retry button clickjackable.
- **No Host check.** `RequireSameOrigin` compares `Origin` against `Host`.
  DNS rebinding defeats that: a page on `attacker.example` that rebinds its
  name to skipper's LAN address is same-origin from the browser's view. It
  can read every GET API and pass the POST guard. A proxy-less homelab
  install, a common setup, is reachable from any website a LAN user visits.
  Its requests still carry `Host: attacker.example`.

## Decision

**Headers.** One middleware wraps the main port and sets, on every response:
`Content-Security-Policy`, `X-Frame-Options: DENY`,
`X-Content-Type-Options: nosniff`, `Referrer-Policy: same-origin`. The policy
is `default-src 'none'` plus `'self'` for script, style, image, font,
connect, manifest and worker, `data:` images (the baked favicon),
`base-uri`/`form-action` `'none'` and `frame-ancestors 'none'`. A handler
that sets its own CSP (the sandboxed icon response) replaces the default.

- **The pre-paint theme script moves to `static/app-prepaint.js`,** loaded as
  a blocking `<script src>` in `<head>`. It was the shell's only inline
  script, so `script-src 'self'` holds with no exceptions. A Go test refuses
  any inline `<script>` or `on*=` attribute in `index.html`.
- **Inline style stays allowed** (`style-src 'self' 'unsafe-inline'`). The
  renderers emit computed `style=` attributes (the phase bar's `flex-grow`)
  and the shell toggles a few `display:none`. With image, font and connect
  sources pinned to `'self'`, injected CSS has no channel out of the page.

**Host allowlist.** An optional `allowed_hosts` list. When set, the UI and its
API answer only a listed `Host`; anything else gets `421 Misdirected
Request`. An entry with a port matches only that `host:port`, one without
matches the name on any port; case-insensitive. `/webhook` and `/healthz`
are registered outside the check: a forge or a probe addresses skipper by
whatever name reaches it, and neither route is a browser surface. An empty
list answers every host, as before.

**Bind address.** An optional `listen_address` (an IP or `localhost`) binds
the main port, so a proxied install can keep the UI off the network
entirely. `metrics_port` keeps listening everywhere; a Prometheus scraper is
usually remote.

## Consequences

- Script injection through a missed escape no longer runs, and the UI cannot
  be framed.
- The pre-paint script costs one more blocking request on a cold load; the
  service worker caches it with the rest of the shell.
- A future inline `<script>`, `on*=` handler or `eval` breaks in the browser.
  The Go test catches the first two in the shell; the e2e layer (Maske BB)
  asserts no CSP violation across the views.
- A primary whose peer sets `allowed_hosts` must reach it by a listed name.
- Installs that do not set `allowed_hosts` remain open to rebinding; the docs
  tell a browser-reachable install to set it.

## Alternatives considered

- **Hash the inline script in the CSP.** No extra request, but the policy
  becomes coupled to the script's exact bytes, comments included, so any
  edit to `index.html`'s head silently breaks the theme until the hash is
  recomputed. A file keeps the policy static.
- **`style-src 'self'` with `style-src-attr 'unsafe-inline'`.** Narrower in
  CSP3 browsers, but older ones fall back to `style-src` and would hide the
  elements the shell toggles. Rewriting every computed style through CSSOM
  buys little once exfiltration channels are closed.
- **Default-deny Host (derive the allowlist).** skipper cannot know the name a
  proxy or a LAN DNS entry gives it, so a derived default would break existing
  installs on upgrade. The check is opt-in and documented instead.
- **`403` instead of `421`.** Either refuses; `421` says the request reached a
  server not configured for that name, which is exactly the case.
