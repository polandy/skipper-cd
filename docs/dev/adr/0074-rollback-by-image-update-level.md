# ADR-0074: rollback gated by the image update level (`rollback: patch_only`)

Status: accepted; amends ADR-0050
Date: 2026-11-24

## Context

ADR-0050 made the git-restore rollback opt-out: `rollback: false` leaves a
failed new version running and held (ADR-0062) instead of restoring the
previous compose file. It is all or nothing per stack. The risk it guards
against — old code against data the new version already migrated — depends on
how far the images moved, though. A patch bump or a rebuilt digest under the
same version tag rarely carries a forward-only migration; a minor or major bump
often does (`postgres 15 -> 16` cannot even read its own data back). An
operator of such a stack has had to choose between losing the rollback for
every config tweak and keeping it for the one update where it does harm.

## Decision

`rollback` takes a third value, `patch_only`, at both levels with the same
resolution as ADR-0050 (per stack over global; `Config.EffectiveRollback`,
resolved once in `deployStacksGated`). In code it is `config.RollbackPolicy`
(`always` = `true`, `never` = `false`, `patch_only`).

Under `patch_only`, `rollBackFailedDeploy` — the one choke point of every
git-restore path (plain `up`, the health gate, `post_deploy`, the rollout
cutover) — compares, per service, the last settled desired reference
(`state.Images[stack]`) with the compose file's (`imageChanges`), and restores
only when no step is larger than a patch:

| Step | Level | Rollback |
|---|---|---|
| same reference (config-only change) | none | yes |
| same version tag, different pinned digest | digest | yes |
| third or later numeric component (`1.2.3 -> 1.2.4`) | patch | yes |
| second component (`1.2 -> 1.3`) | minor | no |
| first component (`15 -> 16`) | major | no |
| non-version tag, shape change, another repository, digest moved under a floating tag, service added or removed, any `build:` service, compose file not parsed | unclassifiable | no |

The largest step across services decides. A skipped rollback behaves exactly
like `rollback: false`: the failed version stays up, the plain error carries
`errNewVersionMark` (held), the event is `failed`, and there is no
roll-forward. The error and the log line name the step
(`rollback skipped: postgres 15 -> 16 is a major update`), so the event, the UI
and the notification say why without a new field.

Tag shapes come from the update check's conservative parser (ADR-0054), moved
into `internal/imageref` together with the reference split
`internal/registry` already did, so there is one notion of a version-shaped
tag. The step size is positional — the first differing numeric component — not
semver's 0.x rule; a downgrade is measured the same way as an upgrade.

The **default stays `true`** (always). `patch_only` treats everything it cannot
classify as too far, so as a default it would silently drop the rollback for
every stack on `latest`-style tags that changes a tag, every stack with a
`build:` service, and every added service — the stateless majority ADR-0050
kept the rollback on for. It is opt-in for the stateful stacks it exists for.

`rollback` stays out of `ConfigHash`: changing the policy never redeploys.

## Alternatives considered

- **A separate key (`rollback_max_update: patch`)** — two keys that must be read
  together, and `rollback: false` plus a level is meaningless. One key with a
  third value keeps the resolution and the validation in one place.
- **Classifying `build:` services by their Dockerfile hash** — an unchanged
  Dockerfile still rebuilds from moved base images and changed sources; nothing
  in the hashes says how far the code moved.
- **Treating a digest move under a floating tag as a patch** — `latest` moving
  can be any step, so it is unclassifiable.
- **Reading the running images (`state.RunningImages`)** — they report what ran
  (ADR-0053) and carry short image ids for floating tags; the decision compares
  desired references, the same input the pull decision uses (Invariant 5).

## Consequences

- With no `patch_only` anywhere, behavior is unchanged.
- A `patch_only` stack whose state has no recorded images (state written before
  images were tracked, or lost) sees every service as new and does not roll
  back until it has settled once.
- A stack on `patch_only` with a `build:` service never rolls back; such a
  stack is better served by `true` or `false`.
