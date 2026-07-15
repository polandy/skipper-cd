# ADR-0027: Live stack health in the UI

Status: accepted
Date: 2026-07-15

## Context

skipper-cd knows whether a *deploy* succeeded, and — with `health_check`
([ADR-0022](0022-health-check-gated-rollback.md)) — whether a stack was healthy
*at the moment it was deployed*. It has no view of a stack's health **afterwards**:
a container that crash-loops or turns `unhealthy` an hour after a green deploy is
invisible in the UI until the next push.

ArgoCD sets the expectation here: it continuously shows the live health of the
resources **it manages**, next to their sync status. The parallel for skipper is
to show the live health of **its own stacks** (the compose projects it deploys)
in the dashboard. That is squarely a *visualization* feature and fits skipper's
viz-only scope — it displays state, it does not trigger or reconcile anything.

This is deliberately narrower than the homelab's separate `system-monitor`
service, and the two are meant to coexist:

| | this feature (skipper) | system-monitor |
|---|---|---|
| Watch surface | **only skipper's own stacks** | **all** host containers |
| Trigger | UI-gated poll, display-only | standalone daemon |
| Node boot/shutdown events | no | yes |
| Alerting / cooldowns | no (out of scope here) | yes |

So the ArgoCD analogy justifies the *display* of own-stack health; the host-wide
watchdog, OS-lifecycle events, and the alerting state machine stay in
system-monitor. See the scope boundary in the Consequences.

## Decision

Add a read-only, **UI-gated health poller** that reports the runtime health of
skipper's own stacks as a new SSE `health` snapshot, rendered as a per-stack
health pill in the UI.

### Reading health

Docker does not push state, so health is **polled**. For each configured stack
the poller runs

```
docker compose -f <composePath> --project-directory <projectDir> ps --format json
```

reusing the *exact* compose-file + `--project-directory` identity the deploy path
already computes (Invariant 1) — the poller never guesses a project name. Output
is captured via the existing `command.ShellRunner.Output` (the deploy `Runner`
only returns an error; the poller consumes a small output-capturing interface,
fake-injected in tests per [ADR-0003](0003-runner-abstraction-and-fake-based-tests.md)).

Compose's per-service `State` (`running`/`exited`/`restarting`) and `Health`
(`healthy`/`unhealthy`/`starting`/none) are rolled up into one per-stack status:

- `healthy` — every service running, and every service with a healthcheck healthy
- `unhealthy` — any service `unhealthy`, `restarting`, or unexpectedly `exited`
- `starting` — any service still `starting` (and none unhealthy)
- `stopped` — no running containers for the project
- `unknown` — the `ps` call or its output could not be read

The rollup, not raw service rows, is what the header/table shows; the per-service
detail is available on demand (see UI surface).

### UI-gated polling

The poller runs **only when the UI is enabled** (its sink is installed), mirroring
the run-plan pass ([ADR-0024](0024-upcoming-deploys-look-ahead.md)) — a headless
deploy does zero health polling. It is further gated on **at least one connected
SSE subscriber**: the ticker loop is idle while nobody is watching the dashboard,
so a UI-enabled but unattended host does not run `docker compose ps` forever. A
client connecting triggers an immediate poll (so it sees health at once), and the
existing post-run hook triggers a poll after every deploy run (so the pill
reflects the just-deployed state without waiting a full interval). The interval is
configurable via `health_poll_interval_seconds` (default 30; `0` disables the
feature).

There is deliberately **no separate enable/disable boolean**. The three gates
above plus `health_poll_interval_seconds: 0` already cover "off", so a fourth,
redundant switch would only add config surface. Per-host control is the NixOS
module option (`healthPollIntervalSeconds`, in the style of `uiTheme`/
`notifications`): a weaker host such as argoneon can raise the interval or set it
to `0` while the NUC keeps the default.

### The `health` SSE snapshot

`HealthSnapshot{ Stacks map[string]StackHealth }` is published over the existing
state-event stream (alongside `autosync`/`queue`/`upcoming`), and the latest
snapshot is served as SSE initial state so a client connecting sees current health
immediately — the same pattern the run plan uses. `_nixos` is not a compose
project and carries no health.

### UI surface

The health pill lives **in the Stack cell**, next to the stack name and icon
(ArgoCD-style "app health beside the app name"), as a small dot + label:
`healthy` green, `unhealthy` red, `starting` amber, `stopped` grey, `unknown`
grey-dashed — semantic colours, a hue kept distinct from the peach `--accent` and
the teal `--success` deploy status. Clicking the pill reveals the per-service
breakdown as a sibling panel below the row, reusing the existing files/diff/error
expand pattern.

To keep an open panel unambiguously tied to its row, the click also **tints the
row and panel** in the stack's health colour with a shared left bar, and the
panel header echoes the stack + status (variant A, chosen over a connector notch
or a plain grouping rail because it names itself and so survives scrolling and
multiple open panels). The bar colour is the rolled-up status — the worst
(least-healthy) service — matching the pill.

**The pill sits only on the newest row per stack, not on every row.** The deploy
table is an *event log* — many rows for the same stack over time — while health is
a single *current* per-stack value. Rendering the live pill on every historical
`gitea` row would repeat the same value and imply the health belonged to that past
deploy; showing it only on the topmost (most recent) row per stack makes it read
as "the current state of this stack", which is what it is. A chosen Stack-cell
placement (over a dedicated Health column) keeps the 5-column grid intact and
avoids spending horizontal width on a value that is only shown once per stack.

The surface is read-only — it shows health, it does not restart or redeploy
anything. `internal/ui/UI_SPEC.md` is updated before the UI change and Andy
eyeballs the rendered pill before the e2e mask is finalized.

## Consequences

- The dashboard answers "is this stack still healthy?" continuously, not just at
  deploy time — the main gap versus ArgoCD's app view is closed for skipper's own
  stacks.
- The deploy path is untouched: the poller is a separate read-only consumer, so
  the change-detection, ordering, and rollback invariants are unaffected.
- Polling reuses the deploy's compose identity, so it cannot drift from what was
  actually deployed; a parse failure degrades to `unknown`, never a false
  `unhealthy`.
- **Scope boundary (the reason this ADR exists).** This feature covers *only*
  skipper's own stacks and is *display-only*. It deliberately does **not** watch
  arbitrary host containers, emit node boot/shutdown notifications, or run an
  alerting/cooldown state machine — those remain system-monitor's job. Notifying
  on a health *change* of an own stack (via the existing `internal/notify` layer)
  is a plausible later step but is **out of scope here**: it turns skipper from a
  viewer into a watchdog and would need its own ADR.

## Alternatives considered

- **Fold system-monitor into skipper.** Rejected: it is a host-wide watchdog with
  a different lifecycle, watch surface, Docker-API access model, and an alerting
  state machine — a whole subsystem, against "small packages, one job", and beyond
  skipper's viz scope. Only the *display of own-stack health* is the in-scope part.
- **Poll unconditionally while the UI is enabled.** Simpler, but runs
  `docker compose ps` on a timer forever even when no one has the dashboard open.
  Subscriber-gating keeps the "do no work nobody watches" property of ADR-0024 at
  the cost of a trivial `HasSubscribers` signal on the broadcaster.
- **One batched `docker ps` instead of `docker compose ps` per stack.** A single
  `docker ps --format json` grouped by the `com.docker.compose.project` label is
  O(1) per interval regardless of stack count — the real resource lever on a weak
  ARM host, more effective than any on/off toggle. It is not the initial choice
  because it trades compose's clean per-service `Health` field for scraping the
  `Status` string (`(healthy)`/`(unhealthy)`/`(health: starting)`), which is
  brittler. Adopt it only if profiling on argoneon shows the per-stack calls
  actually matter; the subscriber-gated default is cheap enough until then.
- **Push health from a compose/Docker event stream** (`docker events`). A live
  stream avoids polling but means holding a long-lived subprocess and parsing an
  open-ended event feed through the Runner abstraction — more moving parts than a
  cheap periodic `ps` for a homelab-scale stack count. Polling is revisited only
  if the interval ever needs to be near-real-time.
