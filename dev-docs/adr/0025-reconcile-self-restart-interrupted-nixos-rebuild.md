# ADR-0025: Reconcile a self-restart-interrupted NixOS rebuild into a success

Status: accepted
Date: 2026-07-15

## Context

A NixOS rebuild runs in a transient systemd unit outside skipper's cgroup so its
`switch-to-configuration` can restart `skipper-cd.service` without killing the
rebuild ([ADR-0014](0014-nixos-rebuild-in-transient-systemd-unit.md)). While
skipper waits for that unit, the wait is bound to a shutdown-aware context: when
the switch restarts skipper, the wait is canceled so the stop does not deadlock.

`rebuildNixOSIfChanged` treated *both* error paths the same way at the end —
whether the wait returned a genuine rebuild error (skipper still alive) or a
`context canceled` because the switch was restarting skipper, it incremented
`skipper_deploy_errors_total` and emitted a `_nixos` **failed** event.

That is wrong for the self-restart case. The rebuild did not fail — it is a
normal outcome: the rebuild keeps running in its transient unit and applies, and
skipper comes back up from the new system. But the last persisted `_nixos` event
stays `failed` (skipped events are not persisted, so nothing supersedes it), so
the UI shows a red `_nixos` even though the system is exactly the configuration
that was built. A burst of consecutive nix-changing deploys makes this reliable:
each one restarts skipper mid-rebuild, so `_nixos` is left failed every time.

Simply emitting a success on the shutdown path is not safe: skipper is being
`SIGTERM`ed, so the event write races the teardown and may not flush.

## Decision

Persist an **in-flight marker** before the rebuild and **reconcile it into a
success on the next startup**, mirroring the pre-saved-hash pattern that already
exists for the same "the switch may restart skipper" reason
([ADR-0005](0005-nixos-hashes-saved-before-rebuild.md)).

- **Before the rebuild**, alongside pre-saving the nix hashes, record the changed
  files in `state.NixOSRebuildInFlight` and persist them.
- **On the shutdown path** (`shutdownRequested()`), do *not* emit a failure or
  count an error — leave the marker set and return. The canceled wait is not a
  rebuild failure.
- **On a genuine failure while skipper is alive**, clear the marker (in addition
  to reverting the hashes per
  [ADR-0015](0015-revert-nix-hashes-on-surviving-rebuild-failure.md)) and emit
  the `_nixos` failed event as before.
- **On success without a restart**, clear the marker and emit success as before.
- **At the start of the next run** (the startup sync after the restart), if the
  marker is set, emit a `_nixos` **success** event for the recorded files and
  clear the marker. The rebuild applied — skipper is running from the new system
  and the pre-saved hashes already match — so the persisted success supersedes
  the missing outcome and the UI goes green.

The marker is a `[]string` (the changed files) rather than a bool so the
reconciled success event still names what changed.

## Consequences

- `_nixos` no longer shows a stale `failed` after a rebuild whose switch
  restarted skipper; it shows the success the interrupted run could not record.
  The false `skipper_deploy_errors_total{stack="_nixos"}` increments are gone.
- The reconciliation is race-free: the success is emitted on a clean startup, not
  during teardown.
- Residual gap (unchanged from before): the marker records *intent*, not a
  verified switch result. If a switch fails *after* it restarts skipper (rare —
  the service restart is late in the switch), the reconciliation still reports
  success. This is the exact correctness profile of the pre-saved hash
  ([ADR-0005](0005-nixos-hashes-saved-before-rebuild.md)): both optimistically
  assume a rebuild that reached the restart step applied. A rebuild failure that
  keeps skipper alive is still caught and reverted (ADR-0015).

## Alternatives considered

- **Emit the success on the shutdown path.** Simplest, but the emit/persist races
  the `SIGTERM` teardown and may be lost. Rejected for the startup reconciliation.
- **Verify the switch result on startup** (query the transient unit, or compare
  the running system's configuration revision to the target). More accurate, but
  the transient unit is usually already gone by the next startup (`Unit not
  loaded`), and the revision comparison duplicates what the pre-saved hash
  already asserts. Not worth the complexity for a gap that matches the accepted
  hash behavior.
