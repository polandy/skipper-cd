# ADR-0051: A bootstrap run converges without refreshing images

Status: accepted
Date: 2026-07-26

## Context

Change detection is driven entirely by `state.yaml`: a stack redeploys when one
of its hashed inputs differs from what that file records (ADR-0002). A run that
finds **no recorded state** therefore sees every input as changed and deploys
every stack. That is documented behaviour ("a missing/corrupt `state.yaml`
means all stacks redeploy — by design") and it is correct for the case it was
written for: a fresh install, where nothing runs yet.

It is not correct for two other cases that reach the same code path:

- **Migration.** An operator moving a host from hand-run `docker compose` to
  skipper already has every stack up, on exactly the versions in the repo.
- **State loss.** A wiped volume, a restored host, a state file that could not
  be parsed. The containers are running; only skipper's memory of them is gone.

The important thing is *which part* of a redeploy actually hurts. `docker
compose up -d` is already idempotent: compose compares its own config hash and
recreates nothing when the container matches. What is not idempotent is the
step before it — `pull`. With nothing recorded, `hasAnyImageChanged` compares
against an empty map, so every image counts as changed and every stack is
pulled. Under a floating tag (`:latest`, `:2`) that moves **every service on
the host** to whatever it resolves to today, unattended, in one run.

That is the exact scenario the README warns about for Renovate automerge
("leave major bumps for a human") — reached by a lost file rather than by a
merge. And rollback cannot help: it recovers a *failed* deploy, not a
successful one that shipped a breaking change.

So the first run after state loss is the moment a homelab operator is most
exposed, and it is triggered by an event rather than by an intent.

## Decision

**A bootstrap run — one that finds nothing recorded — converges the host as
usual but does not refresh images that are already on it.** `pull` is skipped,
and a `build:` service builds without `--pull` (which would refresh its
Dockerfile base image, the same unasked-for jump). Everything else is unchanged:
every stack is deployed, dependency order and the autosync gate apply normally,
and each stack's state is recorded from its own successful deploy.

This needs no configuration and no judgement from the operator, because it
leans on idempotence that Docker already provides rather than on skipper
guessing what "already correct" means:

- **Fresh install** — nothing is running and the images are not on the host, so
  compose fetches each missing image when it creates the container (its default
  `missing` pull policy). Identical outcome to before, minus an explicit pull.
- **Migration / state loss, stacks matching the repo** — the images are
  present and the config hash matches, so `up` recreates nothing. The run is a
  genuine no-op, automatically.
- **State loss where the repo has genuinely moved on** — the config hash
  differs, so `up` recreates from the image already on the host. The stack
  converges to the repo *without* a surprise version jump.

`state.isEmpty()` is evaluated **before** the NixOS phase, which writes its own
hashes into the state ahead of the stack phase (ADR-0005) and would otherwise
make an empty state look populated. The nixos rebuild itself is unaffected:
re-applying a configuration the host already runs is a cheap, idempotent no-op,
and skipping it would risk leaving the host unconverged.

## Consequences

- **No new config key.** An earlier draft of this ADR added
  `initial_deploy: full|adopt`, where `adopt` recorded every stack as deployed
  without running anything. It was dropped: it required the operator to make a
  promise ("these stacks already run the repo's version") that skipper can
  simply avoid needing, and it was **less** correct — a stack whose compose
  file had genuinely moved on would have been recorded as current and never
  converged. Suppressing only the pull keeps convergence intact and removes the
  decision.
- **Suppression lasts exactly one run.** Once state exists, `pull` follows the
  ordinary rule (Invariant 5): it runs when an `image:` reference changed.
- **A bootstrap run can leave an image slightly stale.** If the repo has moved
  on *and* the local image under that tag is older than the registry's, the
  recreate uses the local one. The next change to that stack pulls normally.
  This is the deliberate trade: mildly stale beats a host-wide unattended jump.
- **A compose file that sets `pull_policy: always`** still pulls, because that
  is the operator's explicit instruction in their own compose file.
- **A state directory that cannot be written** makes every run a bootstrap run,
  so images would never be refreshed. The existing `could not save deploy
  state` error covers the cause, and the per-run log line names the mode.

## Alternatives considered

- **Detect running projects via Docker** (`docker compose ps` / config-hash
  labels) and adopt only stacks that match. Rejected: it makes the bootstrap
  depend on a live docker query whose failure mode ("could not read the project
  → deploy it after all") reintroduces the problem, and it re-implements a
  comparison `up` already performs correctly.
- **An `initial_deploy` config key** — see Consequences above.
- **Suppressing the deploy entirely on bootstrap.** Rejected: it leaves a stack
  that is *not* running stopped, and a stack that has drifted unconverged,
  which is the opposite of what a reconciler is for.
