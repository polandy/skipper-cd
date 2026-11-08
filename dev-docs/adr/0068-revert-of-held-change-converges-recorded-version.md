# ADR-0068: Releasing a hold by revert converges the stack to its recorded version

Date: 2026-11-08

## Status

Accepted. Supersedes the mechanism of
[ADR-0066](0066-revert-of-held-change-removes-orphaned-services.md); amends
[ADR-0062](0062-held-changes.md).

## Context

A revert releases a hold (ADR-0062): the stack's inputs match the recorded
hashes again, so the run takes the unchanged path, which makes no `up`.
ADR-0066 added one repair there: containers of services the compose file no
longer declares are removed with `docker rm -f`. It left a declared service
alone, and that gap is not rare:

- With `rollback: false` (ADR-0050) a failed version keeps running by design.
  Reverting it released the hold over containers that still ran the failed
  version, while skipper reported the stack at the recorded one. That is the
  normal path of a supported setting, not a hand-applied corner case.
- A held version applied by hand that changed a declared service's image or
  config kept running it after the revert.
- A held Dockerfile change that reached `up` (with or without rollback) left the
  failed build under the image tag the recorded version uses, and a plain `up`
  of the recorded compose file keeps running it.

## Decision

When an unchanged run finds a hold for the stack, which means it is about to
release that hold, it converges the stack to the recorded version first:

- One `docker compose up -d --remove-orphans` of the clone, which is at the
  recorded inputs, through the same project identity, env and pinned build
  contexts as a deploy (Invariant 1). A stack with Dockerfiles adds `--build`,
  without `--pull`, so the recorded Dockerfile's build replaces the held one's
  and no base image moves unasked (the same reasoning as ADR-0051).
- No hooks, no pull, no health gate, no rollback: the recorded version already
  deployed, so this is a restore like self-heal's (ADR-0029), not a deploy.
- `on_demand_containers` are stopped again right after the `up`, as after any
  deploy.
- The running images are read before and after (ADR-0053). If they differ, the
  run emits one `success` event whose `image_changes` list the difference: a
  service back from the held version reads `old → new`, a service only the held
  version declared reads as removed (empty `new`). No `deploying` event comes
  first, so self-heal's attempt budget is not reset. If nothing differs, or
  either read is unavailable, the run emits the usual `skipped`. The read after
  becomes the running-image baseline.
- If the `up` fails, the run emits a `failed` event and keeps the hold, so the
  next tick tries again. A repeated failure collapses into one row (ADR-0056).

An unchanged stack that is not held is skipped exactly as before, with no
container read and no `up`.

## Consequences

- A revert always leaves the stack running the version skipper reports, with
  rollback on or off, whether the held version was applied by skipper or by
  hand.
- The ADR-0066 `ps`/`rm` path and its "cannot tell, release anyway" degradation
  are gone: the `up` carries the full env, so the `env_files` interpolation
  problem of the bare read no longer decides anything.
- Every hold release costs one `up` and two running-image reads. When the
  rollback already restored the recorded version the `up` is a no-op apart from
  starting and stopping `on_demand_containers` — an on-demand container that was
  in use at that moment is stopped, as on every deploy.
- A declared service that drifted from the recorded version for any other reason
  is recreated by the release too. That is the release's purpose: it is the
  point where skipper claims the stack is at the recorded version again.
- A failing `up` retries every tick while the hold stands, the same cadence
  ADR-0066 accepted for a failing removal.

## Alternatives considered

- **Keep ADR-0066's targeted removal.** It leaves the `rollback: false` revert
  running the failed version, which is the main case.
- **Run the full apply pipeline on release.** Hooks, pull, the health gate and a
  rollback make sense for a new version. Here they would rerun `pre_deploy`
  backups and `post_deploy` migrations for a version that already deployed, and
  a rollback has nothing older to restore.
- **Converge only holds whose new version started.** A hold recorded before
  start (ADR-0064) touched no container, but a hand apply can follow any hold;
  the `up` is cheap and idempotent, so one rule covers both.
