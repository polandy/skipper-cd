# ADR-0017: Self-heal skipper-cd after a failed self-update

Status: accepted
Date: 2026-07-12

## Context

When skipper updates the host it runs on, the `nixos-rebuild` switch restarts
`skipper-cd.service`. ADR-0014 keeps that from deadlocking, and a runner-level
`cmd.WaitDelay` now guarantees the stop returns promptly instead of wedging on
the rebuild's inherited output pipe. But a self-update can still leave the
service **down and latched in `failed`** for reasons outside skipper's control —
e.g. the switch's `switch-to-configuration` being interrupted mid "stopping
skipper-cd.service" (a bare SIGKILL of a wedged process does exactly this), so
the follow-up start step never runs. Observed on both homelab hosts on
2026-07-11: recovery required a manual `systemctl reset-failed && systemctl
start`.

systemd's `Restart=` directive does **not** help here: by design it never acts
on a *commanded* stop (which a switch-driven restart is), nor on a stop the
orchestrator aborted. So no `Restart=` value brings the service back in this
case.

## Decision

Two independent, cheap safety nets in the NixOS module (`module.nix`), on top of
the ADR-0014 / `WaitDelay` fixes:

1. **`Restart = "always"`** (was `on-failure`). Broadens recovery to any
   *spontaneous* exit — including a clean `exit(0)` — while a commanded stop is
   still left alone. With `RestartSec=5s` it does not trip the default start
   limit, so a flapping service keeps retrying rather than latching.

2. **An `autoRecover` timer** (`skipper-cd-recover.timer`, default on,
   `recoverInterval` = 2min). A oneshot restarts the service **only when it is
   in a `failed` state** (`systemctl is-failed`), which catches exactly the
   commanded/aborted-stop case that `Restart=` cannot. An intentional
   `systemctl stop` leaves the unit `inactive` (not `failed`), so it is never
   fought. The check is idempotent and cheap.

## Consequences

- A self-update that ends with skipper down self-heals within `recoverInterval`
  instead of needing a manual `systemctl reset-failed && start`.
- Operators who deliberately stop the service are not overridden (inactive ≠
  failed). Those who want no timer at all set `services.skipper-cd.autoRecover =
  false`.
- A genuinely crash-looping skipper (e.g. broken config) is restarted
  persistently rather than surfacing as a stuck `failed` unit — acceptable for a
  self-hosted CD daemon whose absence is the worse failure, and still visible in
  the journal.
- This is defense-in-depth. The primary self-update robustness lives in
  ADR-0014 (transient unit) and the runner `WaitDelay` (prompt stop); this ADR
  only covers the residual "left down for any reason" case.
