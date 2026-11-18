# ADR-0023: Prompt to reload when a new version is waiting

Status: accepted
Date: 2026-07-14

## Context

[ADR-0018](0018-pwa-installable-ui.md) made the web UI an installable PWA whose
service worker caches the app shell network-first. That keeps a **reloaded** page
fresh: a reachable server always wins, so a plain browser tab picks up a
just-deployed UI on its next navigation.

The gap is the **long-lived standalone window**. An installed PWA can stay open
for days without ever reloading. Because skipper-cd deploys itself, that window
can sit on a stale UI indefinitely with no signal that a newer version is live —
exactly the "stuck on an old build" failure ADR-0018 set out to avoid, just on
the one surface that never triggers the network-first path.

The original service worker installed with `self.skipWaiting()`, so an updated
worker activated immediately and claimed clients. But activating the worker does
**not** reload the open page — the old HTML/JS keep running in memory. So even
with `skipWaiting`, the standalone window showed the old UI until something made
it reload; the automatic swap was invisible to the running page.

## Decision

Turn the silent, page-invisible swap into an explicit, user-driven reload prompt,
entirely client-side (no backend or config change).

### The waiting worker is no longer skipped

The `install` handler drops `self.skipWaiting()`. On an **update** — when a worker
already controls the page — the new worker now parks in the `waiting` state
instead of taking over. On a **first install** there is no controller, so the
browser activates the new worker immediately regardless; the change only affects
updates. The worker activates on demand via a `message` handler that runs
`self.skipWaiting()` when the page posts `{type:'SKIP_WAITING'}`.

### The page detects the waiting worker and prompts

On registration the page watches for a waiting worker (`registration.waiting` on
load, or a new worker reaching `installed` while `navigator.serviceWorker.controller`
is set) and shows a dismissible bottom-centred banner: *"A new version of
skipper-cd is available."* with a **Reload** button. Reload posts `SKIP_WAITING`
to the waiting worker.

### One guarded reload on handover

A `controllerchange` listener reloads the page once when the waiting worker takes
over. It reloads when the update was **accepted** (the user tapped Reload) or a
worker **already controlled the page at load** — the latter covering a second tab
that reloads onto the same version when the first accepts. A `controllerchange`
with neither is just the first-install `clients.claim()` and must not bounce a
fresh visit. (Accepting is tracked explicitly because a page opened before any
worker controlled it — the first-install session — keeps `controller` null for
its lifetime, so a plain "had a controller at load" guard would swallow that
session's own Reload.)

### Actively checking for updates

`registration.update()` is called on load and again on every
`visibilitychange`→visible, so a backgrounded standalone app checks for a new
build when the operator returns to it, rather than only on a manual reload.

## Consequences

- A standalone PWA window can no longer get stuck on an old build: it surfaces a
  clear, tappable prompt and moves to the new version on demand. The action is a
  real button (tap, not a hover title) so it works on touch.
- Updates become **user-acknowledged** rather than silent. This is intentional:
  swapping the UI out from under someone mid-action (reading a diff, watching a
  deploy) is worse than a one-tap prompt. A plain browser tab is unaffected — a
  reload still fetches fresh via the network-first shell.
- The service worker must keep the `SKIP_WAITING` message contract and the
  no-`skipWaiting`-on-install behaviour together; reintroducing `skipWaiting` in
  `install` would strand the waiting worker's message handler and resurrect the
  invisible-swap gap.
- The prompt is best-effort and failure-tolerant like the rest of the PWA layer:
  an insecure (plain HTTP) context has no service worker and simply never prompts.
