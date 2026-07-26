# ADR-0051: Adopt the running stacks when no state is recorded

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
  skipper already has every stack up, on exactly the versions in the repo. The
  first skipper run redeploys all of them.
- **State loss.** A wiped volume, a restored host, a state file that could not
  be parsed. The containers are running; only skipper's memory of them is gone.

Redeploying a stack that is already at the desired version is *mostly* a no-op —
`docker compose up -d` recreates nothing when the config hash matches. What is
not a no-op is what runs first: `compose pull`. Under a floating tag (`:latest`,
`:2`), pulling means every stack on the host can jump to a newer image at once,
unattended, in a single run. That is the exact scenario the README warns about
for Renovate automerge ("leave major bumps for a human"), arrived at by
accident rather than by a merge — and with rollback able to recover a *failed*
deploy but not a successful one that shipped a breaking change.

So the first run after state loss is the moment a homelab operator is most
exposed, and it is triggered by an event (lost file, new host) rather than by
an intent.

## Decision

Add an `initial_deploy` config key with two values, applying **only** to a run
that finds nothing recorded at all:

- **`full`** (default, unchanged behaviour) — deploy every stack. The only safe
  choice when nothing is known to be running; it must stay the default, because
  a fresh install that adopted instead would silently start nothing at all and
  look like skipper doing its job.
- **`adopt`** — record each stack's current inputs (hashes, images, project
  dir) as deployed, without running anything. From the next run the stacks are
  ordinary up-to-date stacks, and the first genuine repo change deploys
  normally.

"Nothing recorded" is `last_deployed_commit == "" && len(stacks) == 0`,
evaluated **before** the NixOS phase — that phase writes its own hashes into
the state ahead of the stack phase (ADR-0005) and would otherwise make an empty
state look populated.

**Adopting covers stacks only, never the NixOS rebuild.** The two cases are not
alike: a compose stack that is already running is expensive and risky to
re-deploy (the pull above), while `nixos-rebuild switch` against a
configuration the host already runs is a cheap, idempotent no-op. Skipping it
would buy nothing and risk leaving the host unconverged, which is the one thing
a reconciler must not do.

**The two gates an adopting run bypasses**, both because it changes nothing on
the host and there is therefore nothing to hold back:

- **Autosync.** Adopting is evaluated *before* the pause gate. Queueing instead
  would leave the stack dirty and deploy it for real on resume — the opposite
  of what adopt was asked to do.
- **Dependency ordering** (ADR-0032). Ordering sequences *deploys*, and an
  adopting run performs none. A stack held back because a sibling's compose
  file failed discovery would stay unrecorded and then deploy for real on a
  later run — reintroducing exactly the unattended pull this mode avoids.

An adopted stack emits the existing `skipped` status. It is the honest one —
nothing was deployed — and it keeps the UI, the audit log and the notification
matrix unchanged. The run additionally logs a `WARN` naming the mode and every
adopted stack, so the one run where skipper deliberately does not converge the
host says so loudly.

## Consequences

- The migration path becomes a two-line config change instead of "redeploy
  everything and hope", and a lost `state.yaml` on a known-good host is
  recoverable without a full pull-and-restart of every service.
- **`adopt` is a promise the operator makes.** A stack that is *not* actually
  running, or is running something other than the repo's version, stays that
  way until one of its files changes. That is exactly the trade being made, and
  the reason `full` remains the default.
- **A state directory that cannot be written turns `adopt` into "never
  deploys".** Every run would find an empty state and adopt again. The existing
  `could not save deploy state` error covers the cause, and the per-run adopt
  `WARN` makes the symptom visible rather than silent.
- A stack **added to the repo later** is unaffected: by then state exists, so
  the run is not an adopting one and the new stack deploys normally.
- Not in `ConfigHash`: `initial_deploy` describes how a run bootstraps, not what
  a stack deploys, so changing it must not redeploy anything (same reasoning as
  `self_heal`, `autosync` and `rollback`).

## Alternatives considered

- **Adopt only stacks whose compose project is currently running**, deploying
  the rest. Needs no operator judgement and is strictly more precise. Rejected
  for now: it makes the bootstrap depend on a live docker query whose failure
  mode ("could not read the project → deploy it after all") reintroduces the
  problem, and it decides for the operator what "already correct" means.
  Reconsider if the explicit key proves too blunt.
- **Change the default to `adopt`.** Rejected: a first install would start
  nothing while reporting every stack as skipped — a much worse failure than
  the one being fixed, and silent.
- **A one-shot CLI flag (`skipper -adopt`) instead of config.** Rejected: the
  state-loss case is unattended (the service restarts on its own), so the
  intent has to be recorded where the service can read it.
