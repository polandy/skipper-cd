# ADR-0050: Per-stack (and global) rollback opt-out for stateful stacks

Status: accepted
Date: 2026-07-25

## Context

A failed deploy is rolled back automatically: the previous `docker-compose.yml`
is fetched from the last deployed commit and re-applied (ADR-0004, ADR-0022).
The rollback fires when `docker compose up` fails, when the health gate fails
(`--wait` timeout or HTTP probe), or when a `post_deploy` hook fails. It is
**unconditional** — there is no way to turn it off. The only related knob is
`deploy_health_check: false` (ADR-0049), which removes the *gate*, not the
rollback that a plain `up` failure still triggers.

Rollback restores the compose file, **not data on disk** (documented as a
warning in `docs/configuration.md`). For a stack that ran an irreversible,
forward-only **schema migration** as part of the failed deploy, restoring the
old image points old code at forward-migrated data — often more broken than the
failed new version, and many migrations can't be undone anyway. For such
stateful stacks an operator wants **fail-loud**: mark the deploy `failed`,
alert, and leave it for manual intervention, rather than auto-reverting to a
version that no longer matches the data. This mirrors the existing "roll out
only stateless stacks" stance (ADR-0040): the automatic, minimize-downtime
recovery paths are unsafe for stacks that mutate persistent state on deploy.

This is a genuine trade-off — *auto-recover / minimize downtime* vs *preserve
data / fail loud* — so it gets a config knob and an ADR rather than a silent
default change.

## Decision

Add a `rollback` boolean, resolved per-stack over a global default, defaulting
to **on** (unchanged behavior). It gates only the git-restore rollback, not the
health *gate* — a stack can keep `deploy_health_check` (so a broken deploy is
still *detected* and marked failed) while opting out of the *restore*.

- **Per-stack** `rollback: false` — this stack is never restored to the previous
  compose version on a failed deploy.
- **Global** `rollback: false` — the default for every stack; a per-stack
  `rollback: true` opts back in. `nil` (omitted) at both levels means on.
- Resolution: `Config.RollbackEnabled(name)` / `EffectiveRollback(stacks, name)`
  — per-stack wins, else global, else on. Mirrors `SelfHealEnabled` /
  `EffectiveSelfHeal` exactly.

**When rollback is off and a deploy fails:** the deploy is marked `failed`
(events, metrics, notifications), the change stays pending so the next sync
retries, and **the failed containers are left running** — `up` already started
them, and skipper does not touch them further. Leaving them running (rather than
stopping the stack) is the least-destructive, most-inspectable outcome: for the
migration case the new code stays up against the data it migrated, and the
operator can read logs / `exec` in live before deciding. It is also the
simplest: the opt-out is a single early return in `rollBackFailedDeploy` before
any restore.

Because every git-restore path (`up` failure, health-probe failure,
`post_deploy` hook failure, and the rollout mid-cutover restore) funnels through
`rollBackFailedDeploy`, the opt-out is one guard at the top of that function. The
rollout *canary-unhealthy* path is unaffected: it never git-restores — the old
container simply keeps serving and the discarded canary is removed — so it stays
`rolled_back` regardless of this flag (it carries no data risk).

The effective policy is resolved once in `DeployAllStacks`, where the global
default is in scope, onto `stack.Rollback` before the deploy loop — the same
resolve-upstream pattern used for the discovered stack set. `rollBackFailedDeploy`
then reads `run.stack.Rollback`; `nil` (unresolved, as in direct unit-test
calls) means on, so the default path is unchanged.

`rollback` is a **runtime failure policy**, not a deploy-shaping input, so it is
excluded from `ConfigHash` (`stackDeployInputs`) — exactly like `self_heal` and
`autosync`. Toggling it must not by itself redeploy an otherwise-unchanged stack.

## Consequences

- Default behavior is unchanged: with no `rollback` key anywhere, every stack
  still rolls back on failure. The feature is purely additive and opt-out.
- A stateful/migrating stack sets `rollback: false` (optionally keeping
  `deploy_health_check` so failures are still detected) and treats a `failed`
  event as the signal to intervene, with the failed version left running for
  inspection.
- Toggling `rollback` never triggers a redeploy (not in `ConfigHash`), matching
  the other runtime toggles.
- No change to Invariant 3's rollback mechanics for the default (on) case; the
  invariant summary gains a note that the git-restore is skipped when `rollback`
  is off, in which case a failed deploy is reported `failed` with its containers
  left running.
