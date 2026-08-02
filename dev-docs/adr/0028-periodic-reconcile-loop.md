# ADR-0028: Periodic reconcile loop

Status: accepted
Date: 2026-07-16

## Context

skipper-cd deploys are triggered from exactly two places: the startup sync
(`SyncAndDeployAll` on boot, to catch changes that landed while skipper was down)
and the webhook handler ([ADR-0009](0009-webhook-branch-filter.md)). Between
pushes skipper is entirely passive — it never looks at the deploy repo on its own.

That makes the webhook a **single point of delivery** for keeping live state in
sync with git. If a delivery is lost, the deploy repo and the running stacks drift
apart until the *next* push happens to arrive or skipper restarts. Realistic ways a
push goes unseen in the homelab:

- skipper is down / restarting exactly when the forge fires the hook (no retry, or
  retries exhausted);
- a transient network blip between gogs and skipper;
- a webhook misconfiguration or secret rotation that silently drops deliveries;
- a change reaching the branch tip through a path that doesn't emit a push webhook
  at all (a forge-side merge, a tag/branch fast-forward, a mirror sync).

In every case the fix today is manual: notice the drift, then push an empty commit
or restart skipper. That is exactly the failure mode GitOps controllers are built
to eliminate. **ArgoCD polls the git repo on a fixed interval (default ~3 min)
even when webhooks are configured** — the webhook is treated as a latency
optimization, and the periodic poll is the correctness guarantee. skipper has the
optimization but not the guarantee.

Adding a periodic reconcile stays squarely inside skipper's scope (the "viz
tool, git-driven, no manual trigger" boundary): it introduces **no new trigger
surface** and **no manual action** — it is the *same* git-desired-state deploy
skipper already does, just on a timer instead of only on a push. It reconciles
against **git**, not against
container runtime state; runtime-drift self-heal is a separate, later question (see
Consequences).

## Decision

Add a **periodic reconcile ticker** that reruns the existing `SyncAndDeployAll` on
a configurable interval, so a missed or lost webhook can no longer leave the host
drifted from the deploy repo indefinitely.

### Mechanism — reuse, don't reinvent

The ticker adds almost no new logic; it leans entirely on machinery that already
exists:

- **Trigger.** A goroutine on a `time.Ticker` calls the same
  `deployer.SyncAndDeployAll(ctx, cfg)` that startup and the webhook call. `Sync`
  fetches the remote branch and resets the clone to its tip (`internal/git`), then
  change detection runs.
- **No-op when nothing changed.** Hash-based change detection
  ([ADR-0002](0002-hash-based-change-detection.md)) means a reconcile against an
  unchanged repo does a `git fetch` + a SHA-256 pass over tracked inputs and then
  deploys **nothing**. The common case is cheap and side-effect-free — no compose
  command runs, no event is emitted.
- **Serialization.** The deploy mutex (Invariant 7, `TryLock`-then-`Lock`) already
  serializes every deploy source. A tick that fires while a webhook deploy is in
  flight does not run concurrently.
- **Autosync / queue respected for free.** Because a reconcile flows through the
  same per-stack deploy gate, a stack with autosync off does **not** get
  force-deployed — a reconcile that finds a change on such a stack queues it
  exactly as a webhook would ([ADR-0016](0016-autosync-and-queue-via-leave-dirty.md)).
  The reconcile loop therefore needs no autosync awareness of its own.

### Skip-if-busy, don't pile up

The ticker uses `TryLock` semantics: if a deploy is already running when a tick
fires, the tick is **dropped**, not queued behind it. A long deploy must not
accumulate a backlog of pending reconciles that all fire the instant it finishes
(that would violate the no-coalescing intent of
[ADR-0010](0010-no-deploy-coalescing.md) — one reconcile is as good as ten, since
each re-reads the current branch tip). The next scheduled tick will reconcile
against the then-current state anyway. This is a behavioural difference from the
webhook path (which waits on the lock so a real push is never lost); a reconcile
tick, unlike a push, carries no unique information, so dropping it is safe.

### Not UI-gated

Unlike the run-plan pass ([ADR-0024](0024-upcoming-deploys-look-ahead.md)) and the
health poller ([ADR-0027](0027-live-stack-health-in-ui.md)) — both of which exist
only to feed the dashboard and so are gated on the UI being enabled and watched —
the reconcile loop is a **correctness feature** and must run on a **headless**
host. It is gated only by its own interval config, never by UI state or SSE
subscribers.

### Configuration

A single `reconcile_interval_seconds` (global, in the style of
`health_poll_interval_seconds`):

- default **300** (5 min), and **on by default** — the feature's entire value is
  covering the case the operator *didn't* notice, so skipper is self-correcting out
  of the box (the ArgoCD posture). 300 s is looser than ArgoCD's 180 s because
  pushes here are usually delivered promptly and the interval only needs to bound
  *worst-case* drift, not be the primary path;
- **`0` disables** the loop entirely, restoring today's pure webhook-plus-startup
  behaviour for anyone who wants it;
- per-host control via a NixOS module option (`reconcileIntervalSeconds`, matching
  `healthPollIntervalSeconds` / `uiTheme`), so a host can widen or disable it.

## Consequences

- A lost webhook is no longer operator-visible drift: within one interval skipper
  re-fetches the branch tip and converges. The single-point-of-delivery weakness of
  the webhook-only model is closed.
- Startup sync becomes a special case of the reconcile loop (the boot-time tick),
  not a distinct concept — though it is kept as an explicit immediate call so a
  restart still converges without waiting a full interval.
- Steady-state cost is one `git fetch` + one hash pass per interval per host, even
  when idle. This is cheap and bounded, and fully disable-able on a weak host via
  `reconcile_interval_seconds: 0`.
- **Scope boundary.** This reconciles against **git desired state only**. It does
  *not* inspect container runtime health, so it will not correct a stack that
  crashed without any git change — that is *runtime* self-heal, a distinct feature
  that would build on the ADR-0027 poller and needs its own ADR. Keeping the two
  separate preserves the clean "this loop only ever does what a push would do"
  property, which is what makes it obviously in-scope.
- Metrics/events are unchanged in shape: a reconcile that deploys emits the same
  events a webhook deploy does; a no-op reconcile emits nothing, so the event log
  is not spammed with "checked, nothing to do" noise.

## Alternatives considered

- **Status quo (webhook + startup only).** Simplest, but leaves the missed-delivery
  drift unaddressed — the exact gap this ADR exists to close.
- **UI-gate the loop like the health poller.** Rejected: reconcile is a correctness
  guarantee, not a display feed. Gating it on a watched dashboard would mean an
  unattended headless host — the one most likely to miss a webhook unnoticed —
  never reconciles, which is backwards.
- **Detect-and-notify without deploying ("drift alert").** Fetch + hash on a timer,
  and on divergence emit a notification instead of acting. Rejected as the default:
  it reintroduces a manual step (someone must see the alert and push), and skipper
  already has the right non-manual answer for "found a change but shouldn't
  auto-apply it" — the autosync-off queue (ADR-0016). Reconcile deferring to that
  gate gives drift-alerting *for free* on stacks configured that way, without a
  separate mode.
- **Queue ticks instead of dropping them (`Lock` not `TryLock`).** Rejected: a
  backlog of reconciles all re-reading the same branch tip is pure waste and
  fights ADR-0010; one timely reconcile subsumes any number of skipped ones.
- **Long-poll / server-side git change stream from the forge.** More timely than a
  fixed interval, but means holding a long-lived connection and forge-specific
  protocol support — far more moving parts than a `time.Ticker` for a homelab that
  already has webhooks for the low-latency path. The interval is the safety net, not
  the primary channel, so near-real-time is not a goal.

## Amendment (2026-07-25): reconcile is the baseline, the webhook is optional

This ADR framed reconcile as the correctness guarantee and the webhook as a
latency optimization, but still assumed a webhook is always configured (the "0
disables the loop, restoring pure webhook-plus-startup" wording above). That
assumption is now dropped: since reconcile alone converges the host, `webhook_secret`
becomes **optional**. An empty secret disables the `/webhook` endpoint (it already
rejects with 403) instead of failing config load, so a host can run reconcile-only.

The one illegal combination is guarded at load: an empty `webhook_secret` **and**
`reconcile_interval_seconds: 0` leaves nothing to deploy past the startup sync, and
is rejected with an actionable error. No new trigger surface, no runtime-state
inspection — the scope boundary above is unchanged.
