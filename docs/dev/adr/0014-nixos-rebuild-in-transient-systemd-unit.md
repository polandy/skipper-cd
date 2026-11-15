# ADR-0014: NixOS rebuild runs in a transient systemd unit

Status: accepted (amended 2026-07-12 — see "Amendment")
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

## Amendment (2026-07-12): the `--wait` client kept the deadlock — fire-and-forget instead

The original decision did **not** actually break the deadlock, and it recurred
in production on the 0.7.0→0.8.0 self-update: the switch left the host on the
old generation with skipper stopped (`skipper-nixos-rebuild.service` exited
`status=120` the instant `skipper-cd.service` was stopped).

**Why the transient unit did not help:** `systemd-run --wait` keeps a *client*
process, and that client is a child of skipper — it lives in
`skipper-cd.service`'s cgroup. So the very deadlock this ADR set out to fix was
still present, just relocated into the client:

1. the rebuild's `switch-to-configuration` runs `systemctl stop skipper-cd.service`;
2. that stop cannot complete until every process in the cgroup exits —
   including the `systemd-run --wait` client;
3. the client will not exit until the rebuild unit finishes;
4. the rebuild is blocked in step 1 waiting for the stop.

`WaitDelay` (below) only made skipper *itself* exit; the client kill it forced
still left the switch half-applied. The transient **unit** was correctly outside
skipper's cgroup — but the **client that waited on it was not**.

**Fix:** run the rebuild *fire-and-forget* — `systemd-run --unit=skipper-nixos-rebuild
--setenv=PATH=… nixos-rebuild switch --flake <flake>` with **no** `--wait`, `--pipe`,
`--collect`, or `--same-dir`. `systemd-run` returns as soon as the unit starts, so
**no client remains in skipper's cgroup** and the stop in step 2 completes
immediately — the deadlock cannot form. skipper then polls the unit to completion
using `systemctl is-active`/`is-failed` **exit codes** (no output capture, so the
`Runner` interface is unchanged); on shutdown the poll is abandoned against the
shutdown context and the detached unit finishes the switch on its own. Dropping
`--collect` is required so a *failed* unit lingers for `is-failed` to observe; a
`systemctl reset-failed` before each run frees the fixed name (a successful unit
is garbage-collected automatically). Implemented in `internal/nixos.Rebuild`
(`waitForUnit`).

Consequence for output/logging: the rebuild's live output no longer streams into
skipper's log pipeline (ADR-0013) — it lives only in the `skipper-nixos-rebuild`
journal unit (`journalctl -u skipper-nixos-rebuild`). This is an acceptable trade
for a switch that cannot deadlock; during a self-restart skipper could not show
the tail of its own restart anyway. The "Implementation note: `WaitDelay`" below
is therefore **superseded** for the rebuild path (WaitDelay stays in the runner
as a general safeguard for any command whose grandchild holds the output pipe).

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
