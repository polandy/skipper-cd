# ADR-0029: Runtime drift self-heal via corrective redeploy

Status: accepted
Date: 2026-07-16

## Context

skipper-cd now converges the host toward **git desired state** from three
triggers: startup sync, the webhook ([ADR-0009](0009-webhook-branch-filter.md)),
and — once [ADR-0028](0028-periodic-reconcile-loop.md) lands — a periodic
reconcile ticker. All three answer the same question: *does the running compose
project match what git says it should be?* None of them looks at whether the
containers are actually **up and healthy**.

Since [ADR-0027](0027-live-stack-health-in-ui.md) skipper *does* know that: the
health poller rolls each stack up to `healthy` / `unhealthy` / `starting` /
`stopped` / `unknown` by reading `docker compose ps`. But that knowledge is
**display-only** — the poller reports a red pill in the dashboard and does
nothing. A stack that crash-loops, turns `unhealthy`, or has a container
`docker stop`'d out of band stays broken until a human notices the pill and
pushes something, exactly the manual failure mode GitOps is meant to remove.

ADR-0028 closes drift on the **git axis** (git changed, live didn't). This ADR is
the follow-on it explicitly deferred — closing drift on the **runtime axis**
(live degraded, git didn't). Together they complete the ArgoCD parallel: ArgoCD
tracks both a *sync* status and a *health* status and can self-heal each.

### The honest counter-question: doesn't `restart:` already do this?

It mostly does, and the analysis has to start here or the feature is
cargo-culted. Docker Compose's own `restart: unless-stopped` / `restart: always`
policy restarts a process that exits, and it is the correct first line of defence
— the equivalent of the kubelet restarting a crashed pod that ArgoCD leans on
rather than duplicating. A stack that simply crashed and has a restart policy set
is **not** skipper's problem to solve.

Self-heal only earns its place on the cases the restart policy does **not**
cover:

1. **A container that was removed or `docker stop`'d out of band.** A restart
   policy cannot recreate a container that no longer exists, or restart one an
   operator deliberately stopped. `compose up -d` recreates it — this is the
   truest "drift from the deployed desired set".
2. **A container that exhausted its restart policy** (`on-failure` with a max, or
   Docker giving up into `dead`). The policy has stopped trying; a fresh `up`
   restarts the reconvergence.
3. **An `unhealthy` healthcheck.** A failing `HEALTHCHECK` does not, by itself,
   make the container exit, so `restart:` never fires — the container sits
   `running (unhealthy)` indefinitely. (Container-level autoheal tools exist for
   exactly this gap.)
4. **A partial stack** where some services are up and one is missing — `up -d`
   reconciles the whole project to its declared service set in one call.

So the value is real but **narrow and additive**, not a replacement for restart
policies. That framing drives every scoping decision below.

## Decision

Add an **opt-in, per-stack self-heal**: when the health poller reports a stack
degraded for a sustained window, run a corrective `docker compose up -d` for that
one stack to restore it to its **currently deployed** desired state — guarded by a
debounce, a cooldown, and a max-attempts circuit breaker. Off by default.

### Restore to *deployed* desired state, not a version change

The heal action is a plain `docker compose up -d` against the stack's
**already-deployed** compose file (the repo clone, Invariant 1) — the same file
that produced the running state. It is **not** a gated deploy and it does **not**
roll back:

- Git has not changed, so change detection is a no-op and the normal deploy path
  would do nothing — self-heal is precisely the "force an `up` even though hashes
  match" case the git-driven path deliberately never triggers.
- Rollback ([ADR-0004](0004-rollback-via-old-compose-file-from-git.md),
  [ADR-0022](0022-health-check-gated-rollback.md)) is meaningless here: there is
  no *newer* version that broke things to retreat from. Rolling "back" would move
  the stack to an *older* commit than the one git currently wants — actively
  wrong. Self-heal restores the current desired version; it never changes which
  version is desired.

This keeps the property that only a git deploy ever changes *what* is deployed;
self-heal only changes *whether it is running*.

### Detection: sustained degradation, not a blip

The poller feeds a self-heal consumer that fires only when a stack is `unhealthy`
or unexpectedly `stopped` for **N consecutive polls** (`min_unhealthy_polls`,
default 3). This debounce keeps skipper from racing Docker's own restart policy:
a container that is briefly `restarting` will usually recover on its own within a
poll or two, and self-heal must not stampede an `up` on top of a restart already
in progress. `starting`, `unknown`, and an *expected* `stopped` (a stack with no
running containers that was never meant to be up) never trigger a heal.

### Circuit breaker: never a hot loop

If the degradation is caused by the app itself — a broken image, a config error,
a healthcheck that will never pass — re-running `up` cannot fix it, and doing so
on every interval is a churn-and-event-spam hot loop. So each stack carries a
per-stack attempt counter:

- after each heal, the **next** poll re-evaluates the stack;
- a heal that leaves the stack still degraded counts as a failed attempt;
- after `max_attempts` (default 3) consecutive failed heals, skipper **stops**
  and leaves the stack reported `unhealthy`, emitting a single
  `heal_exhausted` event (not one per interval);
- a `cooldown_seconds` (default 60) minimum gap separates attempts so even the
  pre-exhaustion attempts are paced;
- the counter **resets** when the stack returns `healthy`, or when a real git
  deploy of that stack happens (a push may have fixed the underlying cause).

This bounds self-heal to at most `max_attempts` corrective `up`s per outage, then
degrades to exactly today's behaviour (report, don't act).

### Serialization and reuse

A heal acquires the **same deploy mutex** every other deploy source uses
(Invariant 7): it never runs concurrently with a webhook, reconcile, or startup
deploy, and a heal in flight blocks (or is blocked by) them. It reuses the
existing single-stack compose invocation (`runDockerCompose`) with the stack's
computed compose-path + `--project-directory` identity — no new compose plumbing.
A heal emits a new `healed` deploy event (source-tagged) so the timeline and
notifications ([ADR-0020](0020-outbound-deploy-notifications.md)) distinguish a
self-heal from a git deploy; `heal_exhausted` is the natural high-signal
notification for "a stack is down and I could not fix it".

### The headless problem: self-heal un-gates the poller

The health poller is deliberately **UI-gated and subscriber-gated**
([ADR-0027](0027-live-stack-health-in-ui.md)): it only exists when the UI is
enabled and only ticks while someone has the dashboard open. That is correct for
a *display* feed but fatal for a *correctness* feature — the unattended, headless
host is the one that most needs healing and the one where nobody is watching.

So, mirroring ADR-0028's "reconcile is not UI-gated" stance: **enabling self-heal
forces the poller to run headless and un-subscriber-gated.** The poller is
instantiated whenever `self_heal` is on *or* the UI health feed is on; the
subscriber gate then guards only the *display publish*, never the *heal
evaluation*. The heal path runs purely off `health_poll_interval_seconds`.

### Configuration

Two knobs, in the established patterns:

- **`self_heal`** — a per-stack boolean with a global default (the autosync shape,
  [ADR-0016](0016-autosync-and-queue-via-leave-dirty.md)), **defaulting off**.
  Per-stack opt-in matters because self-heal is riskier than display: an operator
  wants it on the stateless web front-end, maybe not on the database.
- The pacing constants (`min_unhealthy_polls`, `max_attempts`,
  `cooldown_seconds`) get sensible fixed defaults, overridable globally if a host
  needs to. Cadence rides the existing `health_poll_interval_seconds` — no new
  interval.
- Per-host control via the NixOS module option (`selfHeal`, in the style of
  `healthPollIntervalSeconds` / `uiTheme`), so a weak host can leave it off.

Self-heal is intentionally **independent of the autosync pause**: pause means
"don't apply *git changes* to this stack", a statement about the git axis;
self-heal restores the *already-applied* version and says nothing new about
desired state. Coupling them would overload one toggle with two meanings. A paused
stack with `self_heal: true` still gets restored to whatever it last deployed.

## Consequences

- The dashboard's red pill becomes actionable without a human: skipper recovers a
  stopped/removed/unhealthy own-stack automatically, within `min_unhealthy_polls`
  intervals, and the ArgoCD "self-heal both axes" parallel is complete.
- **First trigger that acts on runtime rather than git.** Every prior automatic
  action reconciled toward git; self-heal reconciles toward *running*. It
  stays inside the "automatic, not
  manual, no new operator trigger surface" boundary (like autosync and reconcile),
  but it is the first to read container state as its input, so it is a genuine
  scope extension and is gated off-by-default precisely for that reason.
- **Bounded and non-destructive.** Worst case is `max_attempts` corrective `up`s
  per outage then silence + one `heal_exhausted` event; it never rolls a version
  back, never loops, never touches a stack the operator didn't opt in.
- **Overlap with `restart:` policies is accepted, not eliminated.** On a stack
  with a restart policy, Docker usually wins the race (recovers within a poll,
  below the debounce) and self-heal never fires — by design. Self-heal is the
  backstop for the four cases in the Context that the policy cannot handle, plus
  the operator convenience of not having to set a restart policy at all.
- Enabling self-heal makes the health poller run on unattended hosts, adding one
  `docker compose ps` per stack per interval even with no UI viewer — the cost
  ADR-0027's subscriber gate was avoiding. This is the price of a correctness
  feature and is bounded by `health_poll_interval_seconds`; hosts that don't want
  it leave `self_heal` off and the gate stands.
- The deploy invariants are untouched: a heal is an ordinary mutex-serialized
  single-stack `up` with no rollback, so change-detection, ordering, and rollback
  semantics are unaffected.

## Alternatives considered

- **Do nothing; rely on Docker `restart:` policies.** The strongest alternative,
  and the right answer for the pure-crash case. Rejected as *complete* because it
  cannot recreate a removed/stopped container, cannot recover an exhausted policy,
  and never reacts to an `unhealthy` healthcheck — the gaps this ADR targets.
  Anyone content with restart policies simply leaves `self_heal` off and gets
  exactly today's behaviour.
- **Fold self-heal into the periodic reconcile loop (ADR-0028).** Tempting — one
  loop — but the two reconcile against different sources (git tip vs. container
  state) and must stay separable: ADR-0028's whole in-scope argument is "this loop
  only ever does what a push would do", which a runtime-driven `up` on unchanged
  git would violate. Keeping them distinct preserves that clean property and lets
  an operator run reconcile without self-heal.
- **Heal through the gated deploy path with rollback (ADR-0022).** Rejected:
  there is no newer version to roll back from; a "rollback" would regress the
  stack to an older commit than git wants. Self-heal is a restore, not a version
  change, so it must not carry rollback semantics.
- **No circuit breaker — heal every interval while unhealthy.** Rejected: an
  app-level fault (`up` can't fix it) becomes an infinite churn-and-event-spam
  loop. The attempt cap + cooldown + reset-on-recovery bounds the blast radius to
  one outage's worth of attempts.
- **On by default.** Rejected: self-heal reads and acts on container runtime — a
  real scope step — and can surprise an operator who stopped a container on
  purpose. Opt-in per stack keeps the default behaviour identical to today and
  puts the runtime-acting decision explicitly in the operator's hands.
- **Detect-and-notify only (a "stack down" alert, no `up`).** A valid smaller
  feature and effectively the *disabled* state plus a notification. Rejected as
  the headline decision because it reintroduces the manual step GitOps removes;
  but `heal_exhausted` gives precisely this alert for the case where auto-heal
  genuinely can't help, which is when a human actually needs to look.
- **A `docker events` stream instead of polling for the heal trigger.** Lower
  latency, but means holding a long-lived subprocess and parsing an open-ended
  feed (the same trade ADR-0027 declined). The existing periodic `ps` is cheap
  enough at homelab stack counts and reuses machinery already built.

## Amendment (2026-07-18): drift detail on the healed event

The original `healed` event carried no payload — no changed files (there are
none) and no diff (a heal is not a version change), so the UI row was a dead end:
not expandable, with nothing to explain *why* it healed. A stack that keeps
healing gave the operator no way to see what drifted.

The healed event now carries the **services that were degraded when the heal
fired** (`DriftedService{name, status}`, `status` ∈ `unhealthy`/`stopped`), taken
from the same health snapshot self-heal already consumes to decide to act. The
Engine passes them through the `Healer` seam (`Heal(ctx, stack, drift)`), and
`HealStack` attaches them to the event. It is small, so — unlike diffs — it rides
the SSE payload directly rather than a fetch-on-demand endpoint.

In the UI the healed row's files cell shows a teal **self-heal badge** in place of
the (absent) files pill; clicking it expands a bound detail panel with the
corrective-redeploy explanation and the drifted-service list (see
`internal/ui/UI_SPEC.md` → Self-heal). This surfaces the heal's cause without
adding any trigger surface — the action stays backend-only and unchanged; only
its *reporting* got richer. Older healed events (no recorded drift) degrade
gracefully to the explanation alone.

## Amendment (2026-07-21): idle on-demand containers are expected `stopped`

The "*expected* `stopped` never triggers a heal" rule above had a gap: it was only
enforced at the rolled-up stack status, which loses per-service detail. A stack
whose `on_demand_containers` skipper stops right after each deploy rolls up to
`stopped`, and self-heal read that as drift and corrective-redeployed it —
directly contradicting the on-demand invariant the health package already honours
(an exited on-demand container classifies as `stopped`, never `unhealthy`, ADR-0027).
Normally invisible because on-demand stacks rarely redeploy, it surfaced when a
config change redeployed every stack at once and self-heal woke each idle
on-demand container.

Self-heal now discounts on-demand-idle services before it decides. It re-rolls the
health snapshot's per-service statuses (which already carry the `on_demand` flag),
skipping any on-demand container that is `stopped`, with the same precedence the
health package uses; a stack made up entirely of idle on-demand containers reads as
healthy, and the drifted-service list omits them too. A genuinely down *non*-on-demand
service in the same stack still heals, and an on-demand container in any other bad
state (e.g. `restarting`) still classifies as usual — the exclusion is exactly the
intended-idle case, no wider. A fully-down stack with no per-service detail falls
back to the rolled-up status unchanged, so nothing else about the policy moves.
