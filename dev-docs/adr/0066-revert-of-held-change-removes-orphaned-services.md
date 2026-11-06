# ADR-0066: Releasing a hold by revert removes orphaned services

Date: 2026-11-06

## Status

Accepted. Amends [ADR-0062](0062-held-changes.md).

## Context

A revert releases a hold (ADR-0062): the stack's inputs match the recorded
hashes again, so the run takes the unchanged path. That path skips the stack
and makes no Docker call, on the assumption that the containers already run
the recorded version. The rollback normally makes that assumption true, since
its `up --remove-orphans` removes whatever the failed version added.

The assumption breaks when the held version is applied by hand. In production
a change added an `act_runner` service to the gitea stack, failed and was
rolled back, and was then applied manually through the stack's systemd unit.
The revert that followed brought the inputs back to the recorded hashes, so
skipper skipped the stack. `--remove-orphans` never ran, and the
`gitea_act_runner` container kept running, outside the stack's compose file and
invisible to skipper. Orphan detection (`internal/orphans`) does not catch this
either, because it classifies whole compose projects and this project is
managed.

## Decision

When an unchanged run finds a hold for the stack, which means it is about to
release that hold, it first compares the project's containers with the compose
file:

- It runs one `docker compose ps --all --format json` read, through the same
  project identity the deploy uses.
- A container whose service the compose file does not declare is orphaned.
  Those containers, and only those, are removed with `docker rm -f <id>…`.
  There is no `up`, so `on_demand_containers` stay stopped and declared
  services are not recreated.
- If something was removed, the run emits one `success` event whose
  `image_changes` list each removed service with an empty `new`. The UI and
  notifications already render that as "removed". No `deploying` event comes
  first, so self-heal's attempt budget is not reset (ADR-0029). If nothing was
  removed, the run emits the usual `skipped`.
- If the removal fails, the run emits a `failed` event and keeps the hold, so
  the next tick tries again. Releasing the hold would leave a known orphan
  running for good. A repeated failure collapses into one row (ADR-0056).
- If the run cannot tell which services are orphaned, it logs a warning and
  releases the hold as before. That covers a failed read, a compose file that
  does not parse, and a deployer with no `Outputter`. Like the running-image
  read (ADR-0053), this read does not carry `env_files`, so a compose file that
  requires one of their variables (`${VAR:?}`) fails it. Keeping the hold then
  would leave the reverted stack held indefinitely.

An unchanged stack that is not held is skipped exactly as before, with no
container read.

## Consequences

- Reverting a hand-applied held change converges the service *set* to the
  compose file without operator action.
- The comparison costs one `ps` read per hold release, not one per stack per
  tick.
- Services that the compose file still declares are not converged. If the hand
  applied version changed one of them (a different image, or with
  `rollback: false` the failed version left running), that service keeps
  running the held version after the revert. Fixing that would take a real
  converge (`up`) on release and is out of scope here.

## Alternatives considered

- **Compare on every skip.** This also covers manual applies that never went
  through a hold. But it adds one `ps` read per stack per tick, and it would
  delete a service someone is deliberately testing by hand within one tick.
- **`up -d --remove-orphans` on release.** This is compose-native, but it also
  starts stopped `on_demand_containers` and recreates drifted services, which
  is more than releasing a hold should do.
- **A new event status for the cleanup.** It would need UI and notification
  work, while the existing removed-service image change already says exactly
  what happened.
