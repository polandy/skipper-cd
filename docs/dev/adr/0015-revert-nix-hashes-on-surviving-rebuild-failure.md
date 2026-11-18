# ADR-0015: Revert nix hashes when a rebuild fails and skipper survives

Status: accepted
Date: 2026-07-11

## Context

ADR-0005 persists the `_nixos` file hashes *before* `nixos-rebuild` runs, so a
switch that restarts skipper does not trigger a redundant rebuild on the next
startup sync. Its accepted trade-off was that a *failed* rebuild also stays
recorded as done, and is only retried by a fixing commit or manual state
removal.

ADR-0014 moved the rebuild into a transient systemd unit, removing the
self-restart deadlock. But that only fixed one *trigger*. Any other rebuild
failure while skipper stays alive — a broken derivation, an evaluation error,
or a `switch-to-configuration` activation error — still leaves the pre-saved
hashes recorded. The next sync then sees unchanged nix hashes, skips the
rebuild, and the change is silently never applied while state claims it is
done. This is the same silent-partial-update failure class as the old
deadlock, reached through a different door.

This bit host-b in production on 2026-07-11: `switch-to-configuration`
aborted with `Failed to get GID for root … Unknown object
'/org/freedesktop/login1/user/_0'` (a logind race while reloading user units).
The rebuild failed, skipper stayed up, yet `_nixos` was marked done.

The pre-save is only needed to survive the *restart* case; when the rebuild
fails and skipper is still running, skipper can perfectly well correct the
record itself.

## Decision

Distinguish the two failure modes using the shutdown context (already held per
ADR-0014):

- **Rebuild fails during shutdown** (the switch is restarting skipper): keep
  the pre-saved hashes — the startup sync must not rebuild again (ADR-0005).
- **Rebuild fails while skipper is alive** (any genuine failure): revert
  `_nixos` to the snapshot captured before the pre-save, so the next sync
  retries the rebuild instead of skipping it. skipper is still running, so it
  persists the revert atomically (ADR-0006).

## Consequences

- Genuine rebuild failures now retry on the next push automatically; they no
  longer need a fixing commit or manual `state.yaml` surgery. This supersedes
  the ADR-0005 consequence "a failed rebuild is not retried" for the
  surviving-failure case; the restart case is unchanged.
- A rebuild that fails identically on every push retries on every push. That is
  correct — it is a real, unresolved failure — and each attempt surfaces a
  failed `_nixos` event, so it stays visible rather than silently stuck.
- The window between the pre-save and the revert is a single synchronous
  rebuild attempt in one process; a crash inside it degrades to the old
  ADR-0005 behaviour (a missed retry), never to a wrong apply.
