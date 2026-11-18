# ADR-0058: A failed git sync is retried before the run gives up

Date: 2026-08-28

## Status

Accepted

## Context

A run starts by syncing the repo clone; if that fails, the whole run aborts
and the host stays on its current state until the next reconcile tick.

That is the wrong shape for the failure that actually happens in practice.
On a host where the git remote is served through a reverse proxy, restarting
the machine — or applying a config change that restarts both the proxy and
skipper — starts skipper within a second of the proxy. The proxy answers
before it has loaded its routes, so the very first `git fetch` gets an HTTP
404 and git exits 128. Nothing is wrong with the credentials, the remote, or
the clone: a second later the same command succeeds.

The consequence is out of proportion to the cause. The startup run — the one
run that has to converge everything, because it is the one that follows a
change — is skipped entirely, and the host stays unconverged for a full
reconcile interval (5 minutes by default). Anything the run feeds is skipped
with it: the first update check reports zero stacks, so the UI shows nothing
to update until the next check hours later.

## Decision

A run retries a failed sync: `syncAttempts` (3) attempts with a linear
backoff (5s, then 10s), so the last attempt happens about 15 seconds in.
Only after the last one does the run abort as before — same error, same
`failed` health state, same log line. A retry is logged at warn with its
attempt number, so a remote that is genuinely gone still narrates itself.

The wait is abandoned when the context is cancelled: a shutdown must not
wait out the backoff.

The retry applies to every sync, not just the first one after start. The
startup run is where it matters, but a reconcile tick that hits the same
window heals 15 seconds later instead of 5 minutes later, and one code path
is easier to reason about than a "first run only" special case.

There is no config key. The delay is a `SyncRetryDelay` seam on
`deploy.Config` that only tests set — the numbers above are short enough to
never need operator judgement, and long enough to cover a container that is
still coming up.

## Alternatives considered

**Order the units on the host.** systemd can start skipper after the proxy
unit, but not after the proxy has loaded its routes — the readiness that
actually matters is not a unit state. It also fixes one host rather than the
behaviour, and the same race exists at boot for any remote that is not up yet.

**Let the next reconcile heal it.** This is today's behaviour, and it does
converge. It just does so up to a reconcile interval late, every single time
the host is rebuilt — the case where converging promptly is most useful.

**Retry the whole run instead of the sync.** A stack deploy that fails is a
real outcome with an event and a rollback behind it; repeating it would hide
failures rather than absorb a transient one. Only the sync is retried.

## Consequences

- A run holds the deploy mutex for up to ~15s longer while a sync is failing.
  Concurrent webhooks wait, as they always do (Invariant 7).
- `Health()` and the `failed` state are unchanged for a remote that is really
  unreachable — they just report it ~15s later.
