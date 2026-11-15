# ADR-0010: Concurrent deploy runs serialize and wait — no coalescing (yet)

Status: accepted
Date: 2026-07-09

## Context

All deploy runs serialize on one mutex (`SyncAndDeployAll`: TryLock, on
contention log + block on Lock — CLAUDE.md invariant 7). When k webhooks
arrive during a running deploy, k goroutines queue and afterwards run
sequentially. Each queued run performs a full cycle: `git fetch` +
`git reset` (one network round trip, typically well under a second), hashing
all tracked files (milliseconds), then per-stack skip decisions.

**Correctness is not at stake.** Every run syncs to the remote HEAD at its
start, so the last queued run always deploys the newest state; the runs in
between are wasted work, not wrong work. The cost of the status quo is
k−1 redundant no-op runs per push burst and linearly growing latency for the
newest push. Goroutine pile-up is bounded in practice: producing queued runs
requires a validly signed webhook, and each waiting goroutine costs a few KB.

The alternative is coalescing: when the lock is held, set a `pending` flag
and return; the lock holder loops while the flag was set. Sketch:

```go
if !d.mu.TryLock() {
    d.pending.Store(true) // one queued run covers all waiters
    return
}
defer d.mu.Unlock()
for {
    d.syncAndDeployOnce(ctx, cfg)
    if !d.pending.Swap(false) {
        return
    }
}
```

This is more efficient but subtly harder to get right: the flag must be
checked under the right ordering so a webhook arriving between the last
iteration's Swap and the Unlock is not lost; the config of the *triggering*
webhook must not leak into a coalesced run started for a different one;
tests for invariant 7 and the event/metric semantics ("n webhooks, 1 run")
all change. It touches the single most safety-critical piece of the
deployer for a benefit that, at homelab push cadence (manual pushes,
occasional CI, bursts of 2–3), amounts to saving one or two sub-second
no-op runs per burst.

## Decision

Keep serialize-and-wait. Do not implement coalescing now. Instead,
instrument the contention: the counter
`skipper_deploy_lock_waits_total` increments whenever a run has to wait for
the lock, making the actual queueing frequency visible in Grafana.

## Consequences

- Invariant 7 and its tests stay unchanged; the deploy path keeps its
  simple single-flight semantics.
- Redundant no-op runs remain possible after push bursts; they are cheap
  and idempotent.
- Revisit this decision when `skipper_deploy_lock_waits_total` shows
  sustained queueing (rule of thumb: regularly more than a handful of waits
  per day, or single runs long enough that queue latency is felt). The
  sketch above is the starting point; it must land with tests covering the
  lost-wakeup race.
