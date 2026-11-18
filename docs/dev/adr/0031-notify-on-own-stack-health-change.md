# ADR-0031: Notify on own-stack health change

Status: accepted
Date: 2026-07-16

## Context

[ADR-0027](0027-live-stack-health-in-ui.md) added a **display-only** health
poller: it reads the runtime health of skipper's own compose stacks and renders a
pill in the UI. It deliberately does *not* alert, and it is gated twice — it runs
only when the UI is enabled, and it polls only while a client is watching. Its
Consequences section flagged the obvious next step and parked it:

> Notifying on a health *change* of an own stack (via the existing
> `internal/notify` layer) is a plausible later step but is **out of scope here**:
> it turns skipper from a viewer into a watchdog and would need its own ADR.

This is that ADR. The motivation is to notify (Signal, via the existing outbound
layer) when one of skipper's own stacks turns unhealthy *after* a green deploy — a
crash-loop an hour later is today invisible unless someone has the dashboard open.

Two hard constraints from the outset:

1. **It must run in the background even when the UI is off.** A watchdog that only
   watches while someone is looking is not a watchdog. So this feature must *not*
   sit behind ADR-0027's UI gate or its subscriber gate. (ADR-0029's self-heal
   already inverted that gating for headless consumers — this ADR reuses that
   inversion rather than re-deriving it.)
2. **It must be well isolated from existing code.** The deploy path, the UI
   poller, and the deploy-event notifier must be untouched in behaviour; the new
   capability lives in its own package with its own lifecycle.

This does *not* try to replace `system-monitor`. skipper still watches **only its
own stacks** — not arbitrary host containers, not node boot/shutdown, no alerting
state machine beyond simple debounce. The host-wide watchdog stays where it is
(see the scope table in ADR-0027). This closes only the own-stacks slice.

## Decision

Add a new, self-contained package `internal/healthwatch` that **consumes the
shared health poller's snapshot feed**, detects per-**service** health
**transitions**, records a bounded per-service history with timestamps and
commit context, and emits an alert on each alert-worthy transition through the
outbound notification layer.

### Riding the shared health poller (the ADR-0029 seam)

The watcher owns **no poll loop**. [ADR-0029](0029-runtime-drift-self-heal.md)
already broke ADR-0027's gating for exactly this class of consumer: the poller
gained `AlwaysPoll` (poll headless; the subscriber gate then guards only the
display `Publish`) and `OnSnapshot` (a consumer feed on the poller goroutine) so
self-heal can watch an unattended host. `healthwatch.Watcher` registers as a
**second `OnSnapshot` consumer** — `main.go` fans the feed out to self-heal and
the watcher — and enabling `health_watch` sets `AlwaysPoll` the same way
self-heal does. So there is exactly **one** `docker compose ps` sweep per
interval, shared by the UI view, self-heal, and the watchdog, and the compose
identity (Invariant 1) stays in the poller — the watcher cannot drift from what
the UI shows or what was deployed.

The watchdog therefore rides the poller's cadence
(`health_poll_interval_seconds`) and, like self-heal, **requires it to be
positive** (config validation enforces this). It is enabled by the presence of
the `health_watch:` section, with deliberately **no requirement to configure a
target**: with no targets the watcher still logs every transition to the journal
and persists the history — a useful record on a host without notifications.

The one touch to `internal/health`: `ServiceHealth` gains the classified
per-service `Status` (previously only the stack rollup was classified; the raw
State/Health strings stay for the UI). Additive and behaviour-preserving for the
existing poller.

### Transition detection, debounce, and flap control

The watcher tracks health **per service** (not per stack rollup — if one service
in a stack is already failing and a second newly fails, the rollup stays
`unhealthy → unhealthy` and would hide the new failure). Each tick it probes all
stacks and compares each service's status to its last accepted status:

- **Act on transitions only**, never on steady state.
- **Debounce**: a new status must persist for `debounce_polls` consecutive polls
  (default 2) before it is accepted. This absorbs a single transient bad probe
  and, combined with the interval, is the flap guard — a service flipping every
  poll never reaches the debounce threshold, so it does not spam. A dedicated
  per-service cooldown was deliberately **not** built at first, with the slow
  flapper (period longer than the debounce window) named as the revisit
  trigger; that trigger fired — see the amendment below for the opt-in
  `alert_cooldown_seconds`.
- **Alert policy**: an alert fires only for a transition **into `unhealthy`**
  ("newly failed") and for **`unhealthy → healthy`** ("recovered"), so a fired
  alert always gets a matching all-clear. Transitions involving `starting` or
  `stopped` are **recorded and logged but never alert** — an intentional
  `docker compose down` must not page. This is also what keeps on-demand
  (Sablier-style) stacks quiet: an exited `on_demand_containers` container
  classifies as `stopped` whatever its exit code (see the ADR-0027 amendment),
  so skipper's own post-deploy stop and every scheduler-driven idle→run→idle
  cycle stay silent instead of paging unhealthy/recovered on each deploy.
- **`unknown` is not a transition.** A failed `ps` (docker hiccup) yields
  `unknown`; the watcher **holds the last known status** for that stack's
  services and does not alert in either direction. This preserves ADR-0027's
  "never a false unhealthy" property and avoids unhealthy/recovered spam from a
  flaky docker socket.
- A service seen for the **first time** (new in compose, or a fresh state file)
  establishes its baseline silently — first observation is never a transition.

### Persisted per-service history — last 10 phases, with `since`

The watcher's accepted statuses are persisted **per service** as a bounded
history of the **last 10 status phases** (a phase = one accepted status with the
time it began), newest first. Persisting is what lets a restart of skipper *not*
blind the detector — and it *removes* the restart-spam problem rather than
trading it away:

- On start the watcher **loads** the persisted history; the first polls compare
  the current status to each service's newest phase, so only a **genuine**
  transition alerts. No baseline replay.
- A transition that happened **while skipper was down** is correctly surfaced:
  `healthy` newest phase → `unhealthy` observed after restart fires a *"newly
  failed"* alert. (This is the deliberate choice to alert on downtime
  transitions, not stay silent over the gap.)
- A service already `unhealthy` before the restart and still `unhealthy` does
  **not** re-alert — a restart never re-pages a known failure.
- A service that **recovered during downtime** emits its *"recovered"* notice.

Only accepted (debounced) transitions append a phase — transient blips never reach
the history, so the timeline is signal, not noise. A new phase is pushed to the
front; the oldest is dropped past 10 (a fixed bound, mirroring the bounded
persisted `internal/events` history — not configurable until something needs it).
Transition detection is unchanged: it compares only the newly-accepted status to
phase[0]; the history is pure append.

**`since` semantics.** Each phase carries `since` = when that status phase began,
RFC3339 truncated to seconds, stored in **UTC** for on-disk stability; the UI
renders local. It is stamped at the **first** poll of the phase (not at the
confirming poll), so it reflects the true change time and not
change-time-plus-debounce; the debounce counter already tracks the pending phase,
so its start time is free. A same-status re-observation leaves `since` untouched;
`unknown` never resets it. A phase's duration is derivable — phase[i] ended when
phase[i−1] began, so `duration(i) = phase[i−1].since − phase[i].since`; only the
oldest retained phase lacks a predecessor.

**Commit context + deploy correlation.** If known, a phase also carries `commit` =
the newest commit that had touched the stack when that phase began. This is
**context, not a causal claim** — a status change in steady state (OOM, disk
full, upstream dependency) is *not* caused by the last-deployed commit, and
stamping it as a cause would be wrong. Causality is instead **derived**: a phase
whose `since` falls within `attribution_window_seconds` (default 300) after that
stack's last successful deploy is *deploy-correlated* (the deploy plausibly
caused it); a phase that began mid-life with no recent deploy carries the same
`commit` as mere context. The correlation is computed from `since` vs. deploy
time when logging/alerting/rendering, **not** frozen as a stored boolean, so the
window can be retuned without re-interpreting old records.

The watcher learns deploys **without reaching into deploy's `state.yaml`** (which
would break isolation): it is registered as one more **deploy-event sink** — the
same composition mechanism the notifier uses — and records, per stack, the last
successful deploy as `{commit, at}` (the event's newest `CommitInfo`; deploy
events carry commit metadata since PR #86). That record is persisted in the
watcher's own state file so it survives a restart — deliberately *not* seeded
from `events.History`, which only exists with the UI on. Interplay worth noting:
stacks with `health_check` gating
([ADR-0022](0022-health-check-gated-rollback.md)) roll a bad deploy back before
the container stays unhealthy, so post-deploy attribution matters most for
ungated stacks, or for failures that pass the gate and degrade shortly after.

**Storage.** Its own file `<state_dir>/healthwatch.yaml`, atomic temp-file +
rename (Go conventions), YAML for consistency with
[ADR-0026](0026-yaml-state-persistence-not-sqlite.md). Owned by `healthwatch` —
deliberately **not** folded into deploy's `state.yaml` — to keep the package
isolated. Only debounced/accepted phases are written; the in-flight confirmation
counter stays in memory.

```yaml
stacks:
  gitea:
    last_deploy: { commit: a1b2c3d…, at: 2026-07-16T14:03:10Z }
    services:
      gitea:                    # newest first, max 10 phases
        - { status: healthy,   since: 2026-07-16T14:03:22Z, commit: a1b2c3d… }
  vaultwarden:
    last_deploy: { commit: a1b2c3d…, at: 2026-07-16T15:45:30Z }
    services:
      vaultwarden:
        - { status: unhealthy, since: 2026-07-16T15:47:05Z, commit: a1b2c3d… }   # since within window of last_deploy → deploy-correlated
        - { status: healthy,   since: 2026-07-16T09:12:00Z, commit: 9f8e7d6… }   # began in steady state → commit is context only
        - { status: unhealthy, since: 2026-07-16T09:08:41Z, commit: 9f8e7d6… }
```

**Missing / corrupt file** = cold start with an empty map: the first poll
**establishes the baseline silently** and does not alert (so a fresh install with
an already-unhealthy stack does not page). This mirrors the forgiving "missing
state.yaml → redeploy all" stance of Invariant 2 — a present file is diffed
against; an absent one is a clean slate.

### Alert delivery — dedicated targets over the existing transport

The delivery seam is decided as **dedicated targets**: a `health_watch.targets`
list in the same shape as `notifications` targets (format/url/prefix/headers/
number/recipients), but **without `on:`** (a health target receives all
alert-worthy transitions). This keeps deploy-notification and health-alert
semantics fully separate — the isolation constraint — at the cost of repeating a
URL in config when one endpoint serves both.

Transport is **reused, not rebuilt**: `internal/notify` gains a `HealthAlerter`
alongside the existing `Notifier`, sharing the package's `Doer`, per-request
timeout, fire-and-forget bounded queue, and request-building helpers — no second
HTTP path, and the existing deploy-event `Notifier` is untouched in behaviour.
The watcher itself stays transport-agnostic: it depends on a narrow, consumer-side
`Alerter` interface (`Fire(Alert)`), fake-injected in tests per
[ADR-0003](0003-runner-abstraction-and-fake-based-tests.md); `main.go` wires the
`HealthAlerter` in.

**Message shape** (signal format; the generic format posts the structured alert
as JSON): the existing host `prefix` is reused, and the message names the stack,
service, transition, duration of the previous phase, and — when deploy-correlated
— the commit:

```
🚨 stack health: vaultwarden/vaultwarden healthy → unhealthy (was healthy 2h13m) — after deploy of a1b2c3d
✅ stack health recovered: vaultwarden/vaultwarden after 4m12s
```

### Config

A new top-level block, isolated from `notifications`:

```yaml
health_watch:                      # cadence = health_poll_interval_seconds (must be > 0)
  debounce_polls: 2                # consecutive confirmations before a transition is accepted
  attribution_window_seconds: 300  # a phase beginning within this window after a deploy is deploy-correlated
  targets:                         # optional; same shape as `notifications` minus `on:`
    - format: signal
      url: http://localhost:8020
      number: "+41..."
      recipients: ["+41..."]
      prefix: host-a
```

There is deliberately no `interval_seconds` of its own — the watchdog rides the
shared poller's `health_poll_interval_seconds` (like self-heal), and validation
rejects `health_watch` when that is 0. Omitting the whole section disables the
feature, so existing installs are unaffected. Per-host control follows the
established pattern (a NixOS module option in the style of
`healthPollIntervalSeconds`/`uiTheme`): host-b can disable it while host-a
runs it. The history bound (10) is intentionally not exposed.

### Observability — logging + metrics

Every accepted transition is **logged** via the existing `slog` logger (the same
one `notify` and `main` use — structured, to stderr, i.e. the systemd journal),
regardless of whether an alert target is configured. So the transition history is
visible in the journal even on a host with notifications off. One structured line
per accepted transition:

```
level=WARN msg="stack health transition" stack=vaultwarden service=vaultwarden from=healthy to=unhealthy since=2026-07-16T15:47:05Z commit=a1b2c3d deploy_correlated=true
```

with `level=WARN` for `→ unhealthy` and `level=INFO` for everything else.
The fields mirror the persisted phase record and the alert payload, so the
journal, `healthwatch.yaml`, and the Signal message all tell the same story.
`unknown` holds (no transition) and is logged at most at `DEBUG` to avoid noise
from a flaky docker socket.

New counters in `internal/metrics`: health transitions observed (labelled by
resulting status) and health alerts sent (labelled by format and outcome,
mirroring `NotificationsSent`).

### UI surface — follow-up PR

The per-service breakdown panel from ADR-0027 will gain, per service, the current
`{status, since}` inline (e.g. `unhealthy · 6h12m`), the last-10-phase timeline,
and the commit on deploy-correlated phases — via a **lookup-join** onto the
existing panel from a new SSE field published from the watcher's state, so
`internal/health` keeps no notion of `since` and the two loops stay separate.
With the watcher off, the panel degrades to today's live status. Per the UI
workflow (manual eyeball before the e2e mask; one mask per PR), the UI surface —
including the SSE getter — ships in a **separate follow-up PR**; this ADR's
implementation PR is backend-only and deliberately exposes no unused API.

## Consequences

- The main remaining ArgoCD gap for skipper's own stacks — *alert* on live health,
  not just *show* it — is closed, without turning the UI poller into a watchdog.
- **This is a scope shift, and it is bounded on purpose.** skipper becomes a
  watchdog for **its own stacks only**. It still does not watch host containers,
  emit node-lifecycle events, or run a cooldown/incident state machine —
  system-monitor keeps all of that. The two coexist exactly as ADR-0027's table
  describes; this ADR moves only the "alert on own-stack health" cell.
- The deploy path, the `internal/health` UI poller's loop, and the deploy-event
  notifier are behaviourally untouched. The shared code is the pure `Probe`
  function (extracted for DRY compose-identity), the notify transport helpers,
  and a read-only registration on the existing deploy-event sink composition.
- Every accepted transition is recorded in **three** places that agree — the
  journal (`slog`), `healthwatch.yaml`, and the optional outbound alert — so the
  timeline survives even with no targets configured.
- The watcher owns a small persistent state (`healthwatch.yaml`) — the one place
  this feature writes to disk. It carries per-service phase history with `since`
  and commit context plus the per-stack last deploy, survives restarts, and
  degrades to a silent baseline when absent.
- Commit information is recorded as **context**, and causality is a **derived**
  correlation (within `attribution_window_seconds` of a deploy), never a stored
  causal claim — the record cannot assert that a steady-state failure was caused
  by an unchanged commit.
- One `docker compose ps` sweep per interval serves the UI view, self-heal, and
  the watchdog — enabling the watchdog adds **no** additional docker work, only
  the `AlwaysPoll` headless cadence when nothing else already forces it. The
  batched-`docker ps` lever from ADR-0027 remains the escape hatch if an ARM
  host ever feels the per-stack calls.

## Alternatives considered

- **A separate, always-on poll loop owned by the watcher.** The watcher runs its
  own ticker and probes independently (via an extracted `health.Probe`),
  buying total isolation from the poller at the cost of a **second**
  `docker compose ps` sweep per interval. This was the initial design of this
  ADR — written before [ADR-0029](0029-runtime-drift-self-heal.md) landed — when
  reusing the poller would have meant inverting ADR-0027's gating just for this
  feature. ADR-0029 then made that inversion (`AlwaysPoll`) and the consumer
  feed (`OnSnapshot`) part of the poller anyway, which removed the entire
  rationale: rejected in favour of consuming the shared feed.
- **A second, ungated `health.Poller` instance.** Reuses the existing Poller with
  `HasSubscribers: nil`, feeding its `Publish` callback into a transition detector.
  Rejected: two poller instances still double the docker work, and bending
  `Publish` (the UI display path) into an alerting feed muddies its job —
  `OnSnapshot` is the purpose-built consumer seam.
- **In-memory baseline, no persistence.** Simpler, but a restart re-establishes the
  baseline from the first poll — so a service that failed *while skipper was down*
  is read as normal and never alerts, and no "recovered" is emitted for a recovery
  during downtime. Rejected: persistence both fixes this and removes restart spam.
- **Stack-rollup granularity instead of per-service.** Fewer records, but hides a
  second service failing while the stack is already `unhealthy`, and loses
  per-service `since`/history/commit. Rejected — the per-service detail is the
  point.
- **Store commit as a causal `caused_by` claim, or freeze a `deploy_correlated`
  boolean.** Rejected: steady-state failures are not commit-caused, and a frozen
  boolean can't be retuned. Commit is stored as context; correlation is derived
  from `since` vs. deploy time against a configurable window.
- **Reach into deploy's `state.yaml` for the running commit, or seed from
  `events.History`.** Rejected: the former couples the watcher to deploy state;
  the latter only exists with the UI on. Consuming the deploy-event feed and
  persisting `last_deploy` in the watcher's own file is the isolated seam.
- **Reuse `notifications` targets with health statuses in their `on:` set.**
  Least config duplication, but mixes deploy and health semantics on one target
  and widens the deploy-status vocabulary. Rejected in favour of dedicated
  `health_watch.targets`.
- **A native health event through `events` + `notify`'s `Notifier`.** Cleanest
  formatter reuse, but widens the deploy-event vocabulary with a non-deploy
  concept and threads health through deploy-event plumbing. Rejected; the
  `HealthAlerter` reuses the transport without touching the event vocabulary.
- **Gate the watcher on having ≥1 target.** Rejected: journal logging + persisted
  history are valuable on their own (and host-b currently has no local
  signal-api); omitting the `health_watch` section is the off switch.

## Amendment (2026-07-17): per-service alert cooldown

The original decision left a dedicated cooldown deliberately unbuilt, naming a
**slow flapper** — a service whose flap period is longer than the debounce
window — as the revisit trigger: every cycle passes the debounce, so every
cycle pages fail *and* recovery. This amendment adds that cooldown, on by
default.

### Semantics

- **Config**: `health_watch.alert_cooldown_seconds` (default `1800` = 30
  minutes when omitted; must be ≥ 0). An explicit `0` disables the cooldown
  and keeps the original behaviour bit-for-bit — no delivery records are kept
  and `healthwatch.yaml` is unchanged. The field is a pointer in config so an
  omitted field (→ default) and an explicit `0` (→ off) stay distinguishable,
  the `health_poll_interval_seconds` pattern.
- **Per service *and* per direction**: the cooldown is the minimum gap between
  delivered `→ unhealthy` alerts of one service, and independently between its
  `recovered` alerts. An ordinary incident (one failure, one recovery) is
  therefore never delayed — the all-clear pairing of the original decision
  survives intact. Only the *repeat* of the same direction within the window
  is suppressed.
- **Suppressed ≠ lost**: a suppressed transition is still accepted, journaled
  (`msg="health alert suppressed by cooldown"`), persisted in the phase
  history, and counted by a new metric
  `skipper_health_alerts_suppressed_total{status}`. Only the outbound delivery
  is held back.
- **Catch-up — a flap must never settle silently down.** Suppression sets a
  persisted per-service marker. On every later snapshot, once the direction's
  cooldown has expired, the watcher compares the service's current accepted
  status with the operator's picture (the status of the most recent delivered
  alert). If they still diverge alert-worthily — the service sits `unhealthy`
  but the last delivered alert said recovered, or vice versa — the owed alert
  is delivered late, describing the current phase truthfully (`since` is when
  the phase began). A converged service, or one that settled in a silent
  status (`starting`/`stopped`), resolves the marker without paging. So the
  worst case under cooldown is a *late* page, never a missing one, and a
  persistent flapper pages at most one fail/recovery pair per cooldown window.
- **Restart-safe**: the delivery records (`alerts:` map in `healthwatch.yaml`
  — `unhealthy_at`, `recovered_at`, `suppressed` per service) are persisted
  next to the phase history, so a skipper restart mid-flap neither re-pages
  early nor forgets an owed catch-up. Old state files without the map load
  as "never alerted" — the first transition after upgrade delivers normally.

### Alternatives considered (amendment)

- **One cooldown per service across both directions.** Simpler record, but it
  delays the all-clear of an ordinary incident (recovery within the window of
  its own failure alert would be suppressed) — breaking the "a fired alert
  always gets a matching all-clear" decision. Rejected.
- **Cooldown on `→ unhealthy` only, recovery always delivered.** Keeps the
  pairing trivially, but a flapper then still pages "recovered" every cycle —
  half the spam remains. Rejected.
- **Suppress without catch-up.** Smallest change, but a flap whose *last*
  suppressed transition lands `unhealthy` leaves the operator's final
  information as "recovered" while the service is down — a silently-down
  watchdog is worse than a noisy one. Rejected.
- **Opt-in default (`0` = off).** Considered to avoid changing alert timing
  for existing installs, but rejected: the cooldown only ever suppresses
  redundant re-pages of an already-reported direction, and the catch-up
  guarantees eventual delivery — so a sensible default benefits every install
  while an explicit `0` remains the off switch.
