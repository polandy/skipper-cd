# ADR-0014: NixOS rebuild runs in a transient systemd unit

Status: accepted
Date: 2026-07-10

## Context

When skipper updates the very host it runs on, `nixos-rebuild switch` ends
up restarting the skipper service (new package, changed unit). As a direct
child process this deadlocked in production:

1. skipper starts `nixos-rebuild switch` and waits for it,
2. `switch-to-configuration` restarts `skipper-cd.service` → systemd sends
   SIGTERM,
3. graceful shutdown (ADR-0007) waits for the running deploy — which *is*
   the rebuild — while the rebuild's switch waits for the unit to stop,
4. after `TimeoutStopSec` systemd SIGKILLs the whole cgroup, taking the
   half-finished switch down with it.

Because nix hashes are persisted before the rebuild (ADR-0005), the killed
update was also silently recorded as done. Two earlier self-updates only
survived this race by timing luck.

## Decision

- The rebuild runs as a transient systemd unit outside skipper's cgroup:
  `systemd-run --unit=skipper-nixos-rebuild --collect --wait --pipe
  --same-dir --setenv=PATH=… nixos-rebuild switch --flake <flake>`.
  `--wait` propagates the exit code (rebuild failures still abort stack
  deploys), `--pipe` keeps the output in skipper's log pipeline
  (ADR-0013), `--collect` cleans up a failed unit so the fixed name stays
  reusable.
- The deployer holds the process shutdown context
  (`SetShutdownContext`). When shutdown is requested mid-rebuild, the wait
  is abandoned — the transient unit keeps running — the run counts the
  rebuild as failed, deploys no stacks, and exits promptly. The restarted
  service's startup sync then deploys the stacks (nix hashes already match,
  so the rebuild is not repeated).

## Consequences

- Self-updates cannot deadlock the service stop anymore; the switch
  completes independently of skipper's lifecycle.
- Rebuild logs live in the `skipper-nixos-rebuild` journal unit as well as
  skipper's own output.
- If a rebuild is still running when another is dispatched, the fixed unit
  name makes `systemd-run` fail loudly; the run aborts and the next webhook
  retries. Deploy runs already serialize (invariant), so this only happens
  across an abandoned wait.
- `systemd-run` must be on the service PATH; the NixOS module already adds
  `pkgs.systemd` when `nixosRebuild` is enabled. Non-systemd hosts cannot
  use `nixos_rebuild` — which they could not meaningfully anyway.

## Implementation note: `WaitDelay` closes the inherited pipe

"Abandoning the wait" cancels the command's context, which kills the
`systemd-run` *client*, but that is not enough to make `runNixOSRebuild`
return. `--pipe` gives the transient unit this process's stdout/stderr, and
when the UI is enabled those are an `os.Pipe` (a line sink tees child output
into the log ring, ADR-0013). Go's `cmd.Wait` blocks until the pipe reaches
EOF — i.e. until *every* holder of the write end exits. The transient unit
keeps running by design and keeps that write end open, so `Wait` would hang
forever and wedge shutdown (observed in production on a 0.6.0 self-update:
skipper stuck at "waiting for in-flight deploy to finish", the switch blocked
on stopping `skipper-cd.service`, only freed by a manual SIGKILL).

The runner therefore sets `cmd.WaitDelay` (`internal/command`): once the
context is cancelled (or the process exits), `Wait` waits at most that long for
the pipes to drain, then force-closes them and returns. Shutdown proceeds, the
switch completes, and the restarted service's startup sync deploys the stacks.
`WaitDelay` only starts counting after exit/cancellation, so a healthy
long-running rebuild is unaffected.
