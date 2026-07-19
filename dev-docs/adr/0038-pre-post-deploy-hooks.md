# ADR-0038: Pre-/post-deploy hooks

Status: accepted
Date: 2026-07-19

## Context

The motivating need is **backup-before-update**: dump a database (or snapshot a
volume) *before* a stack's images change, while the old containers are still
running, so a bad update can be recovered from by hand. Today skipper-cd has no
place to run such a command — the only escape hatch is baking it into a compose
service, which does not fit "run exactly once, right before this stack rolls."

A secondary need is a post-`up` smoke test or one-shot migration.

This is the homelab cut of ArgoCD's sync hooks (PreSync/PostSync). Alternatives
considered and rejected: global (cross-stack) hooks (`depends_on` already orders
stacks; a per-stack backup is what people actually want); a dedicated backup
feature (skipper-cd stays a thin CD tool — a shell command the user already
knows how to write is more flexible than any backup DSL we would invent);
argv-array hook commands (kills the pipes/redirects/`$(…)` a real backup line
needs).

## Decision

An optional per-stack `hooks` section runs shell commands at two points in the
per-stack deploy sequence:

```
pre_deploy hooks
  → pull → build → up -d [--wait] → HTTP probe
  → post_deploy hooks
  → stop on-demand containers → save state
```

```yaml
stacks:
  - name: paperless
    hooks:
      pre_deploy:
        - "docker exec paperless-db pg_dump -U paperless | zstd > /backup/paperless-$(date +%F-%H%M).sql.zst"
      post_deploy:
        - "curl -fsS http://localhost:8000/api/health/"
      timeout_seconds: 120   # optional; per-hook, capped by command_timeout_seconds
```

- **`pre_deploy` runs before any container is touched.** For an image bump the
  old container is still up, so `docker exec … pg_dump` captures the running
  old version — this is what makes it a valid backup point. If a `pre_deploy`
  hook fails, the deploy aborts before `up`: `failed` event, hashes not saved,
  **no rollback** (nothing changed), next sync retries. *A failed backup means
  the update never starts.*
- **`post_deploy` runs after both health gates pass.** A failing `post_deploy`
  hook enters the **existing rollback path** (ADR-0022) — identical to an HTTP
  probe failure — **even when the stack has no `health_check`.** A post_deploy
  hook the user wrote *is* a health gate; making rollback conditional on an
  unrelated `health_check` block would give the hook two meanings and surprise
  whoever wrote a validation expecting it to matter. One mental model:
  *anything after `up` that fails, rolls back.* Without `health_check` the
  restore `up` uses the no-`--wait` leg, exactly as a probe failure does today.
  Post hooks are not re-run against the restored version.

Each entry is one `sh -c` command line run **through `command.Runner`**, so
hooks get the log pipeline (ADR-0013), the per-command timeout budget, and the
recording fakes in tests. This is the one deliberate `sh -c` in the codebase.
The hook environment is the stack's deploy env (Invariant 6 precedence:
`env_files` > `vars_file` > `os.Environ()`), plus `SKIPPER_STACK` and
`SKIPPER_HOOK` (`pre_deploy`/`post_deploy`) so a shared backup script can branch
on context. The working directory is the stack's effective project directory.

**Hooks are excluded from change detection.** They are not hashed inputs
(Invariant 2 is untouched) and, in stack-discovery mode, `hooks` is excluded
from `Stack.ConfigHash` — joining `icon`/`self_heal`/`depends_on` on that list.
Editing a backup command therefore does not itself redeploy the stack; the new
command takes effect on the stack's next real deploy. A hook that invokes a
script committed under a `watch_dirs` entry is the exception — the script's
content is already hashed, so changing it redeploys.

Hooks run **only when the stack actually deploys** (changed hashes → rollout).
A skipped/unchanged stack, a self-heal `up` (ADR-0029), and the restore leg of a
rollback all run zero hooks.

## Consequences

- Backup-before-update works with one shell line and no new backup machinery;
  the user owns rotation/retention/destination in the command itself.
- **No automatic restore-on-rollback.** An `on_rollback` hook that e.g.
  `psql < backup.sql` was considered and deliberately dropped, not just
  deferred: auto-restoring a database right after a failed deploy clobbers live
  data before anyone has looked at what broke. A rolled-back deploy already
  emits `rolled_back`/`rolled_back_unhealthy` and fires the existing alert
  (ADR-0020/ADR-0031); the operator restores by hand from the `pre_deploy`
  dump after inspecting. That alert is the whole "you need to act" signal — no
  extra hook is needed for it.
- Hooks run under the deploy mutex (Invariant 7), so a slow backup delays that
  sync like a slow `pull` does. `hooks.timeout_seconds` bounds each hook
  *downward*: hooks run through the same runner as every docker/git command, so
  the global `command_timeout_seconds` is the hard per-command ceiling and a
  `timeout_seconds` larger than it has no extra effect (0 means "bounded only by
  the global"). A backup slower than `command_timeout_seconds` needs that global
  raised — the simplest, most consistent knob, since it already bounds `pull`
  and `up`. A no-second-runner design was chosen deliberately over letting a
  hook exceed the global ceiling. A timed-out hook counts as a failure.
- The `sh -c` execution means hook strings are shell-interpreted on the
  skipper-cd host — same trust boundary as the compose files and `vars_file`
  the repo already controls, so no new privilege is granted, but it is
  documented as running with skipper-cd's privileges.
- Because hooks are excluded from hashing, a config with only a hook change and
  no other tracked change will not deploy until something else in the stack
  changes — matching how `icon`/`depends_on` edits behave.

## Amendment (2026-07-19): UI surface

The initial feature (above, #139) is backend-only. A follow-up increment surfaces
hooks in the web UI, read-only, reusing existing surfaces rather than adding
streaming machinery: (1) a **hooks badge** on the stack cell (newest deploy row +
roster row) whose panel lists the configured commands, driven by a `hooks`
count on the `stacks` roster snapshot + a thin `GET /api/hooks/{stack}`;
(2) a **running-hook phase** sub-label on the deploying row, driven by a new
lightweight `hookrun` SSE snapshot published by `runHooks` (UI-sink-gated, like
`upcoming`); (3) **hook log** reuse — `runHooks` tags its child-process lines
with `stack`/`hook` attrs so the existing log view attributes and filters them,
and the running indicator links there. Full surface in `internal/ui/UI_SPEC.md`
(§ Deploy hooks) and `dev-docs/deploy-hooks-spec.md` (§ UI surface). No change to
the deploy semantics above.

## References

- Spec: `dev-docs/deploy-hooks-spec.md`
- Builds on: ADR-0004 (rollback), ADR-0013 (log pipeline), ADR-0022
  (health-check-gated rollback), ADR-0034 (stack discovery / `ConfigHash`).
