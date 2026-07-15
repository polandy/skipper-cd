# ADR-0028: Trusted-proxy and PWA-token auth for the web UI

Status: accepted
Date: 2026-07-15

## Context

The web UI's data API (`/api/*` — deploy events, logs, autosync state, stack
health) has so far been unauthenticated. Deployments are git-driven, so this is
a *visualization* surface, but it still exposes operational detail and a couple
of state-changing endpoints (`POST /api/autosync`, `POST /api/icons/refresh`).
The intended deployment has been "put skipper-cd behind a reverse proxy that
authenticates users" (e.g. Authelia forward-auth).

We want skipper-cd to be able to authorize clients **itself**, for two setups:

1. **Behind a proxy** that has already authenticated the user and can pass that
   fact along (the Immich `x-immich-*` / proxy-header pattern,
   [immich#1604](https://github.com/immich-app/immich/discussions/1604)).
2. **Directly exposed** (no forward-auth proxy), where the user should be able
   to unlock the UI by entering a secret — including from the installed PWA.

This is access control, not a deploy trigger, so it stays within skipper's
viz-only scope.

## Decision

Add an optional `auth` config section and an `internal/auth.Gate` that wraps the
data-API routes. A request is authorized when **either** path succeeds:

- **Proxy path** — the request's real network peer is within `trusted_proxies`
  (CIDRs or bare IPs) **and** it carries a non-empty `trusted_header`. Anchoring
  on the actual TCP peer (`RemoteAddr`), not a forwardable header like
  `X-Forwarded-For`, is what stops a direct client from spoofing the header.
- **Token path** — the request presents a shared `token`, either in the
  `skipper_auth` cookie or as `Authorization: Bearer <token>`, compared in
  constant time (`crypto/subtle`).

Both paths are optional and independent; with neither configured the gate is a
no-op and the UI stays open (backwards compatible). The gate **fails closed**:
with `auth` set, a request satisfying no path gets `401`, whose JSON body
carries a `tokenAuth` hint so the UI knows whether to offer a token field.

### What is gated, and what is not

Only `/api/*` is gated. The app **shell** (`GET /{$}`, the manifest, the service
worker, the PWA icons) stays open, and so do `POST /webhook` (its own HMAC) and
`GET /healthz` (liveness). The shell carries no deploy data — identical bytes for
everyone — and it *must* load unauthenticated, otherwise the PWA cannot install
and there is nowhere to render the token-login screen. The sensitive state all
lives under `/api/*`, so gating there is sufficient.

### PWA token delivery: a cookie, not a header

A PWA issues three kinds of requests: navigations (the HTML, `sw.js`, icons),
`fetch()` calls, and the SSE `EventSource` stream. Only `fetch()` can set a
custom header; navigations and `EventSource` cannot. A cookie is the only
credential the browser attaches to **all three**, so the login screen stores the
token in the `skipper_auth` cookie (`SameSite=Lax`, one year, `Secure` on HTTPS)
after validating it via the bearer path. `SameSite=Lax` blocks cross-site POSTs
(the state-changing endpoints) while allowing the app's own navigations. The
cookie is not `HttpOnly` because the page's JS sets it; acceptable for a homelab
viz tool. Sign-out clears the cookie from the view-options popover.

## Consequences

- New leaf package `internal/auth` (no skipper deps); `internal/config` gains an
  `AuthConfig` and validation (a `trusted_header` requires ≥1 valid
  `trusted_proxies`; proxies without a header are rejected).
- `webhookMux` mounts the data-API routes on a sub-mux behind `gate.Wrap`, while
  the shell and operational endpoints stay on the parent mux.
- The token must be **cookie-safe** (hex/base64) since it is stored verbatim in
  a cookie; documented in `docs/configuration.md`.
- Not built: user accounts, sessions/expiry, or multiple tokens. A single shared
  token (or upstream proxy identity) matches the tool's scale; revocation is
  "rotate the token / cookie".
- `401` (not `403`) is returned so the UI treats it as "authenticate here" and
  shows the login overlay.
