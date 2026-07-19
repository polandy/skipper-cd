# Feature Spec: Pre-/Post-Deploy Hooks

Status: implemented
Date: 2026-07-18 (revised 2026-07-19)

## Goal

Per-stack shell commands around a deploy — the homelab cut of ArgoCD's sync
hooks (PreSync/PostSync).

The **primary, motivating use case is backup-before-update**: dump a database
(or snapshot a volume) *before* its stack updates, while the old containers are
still running, so a bad image can be recovered from. A secondary use is a
post-`up` smoke test or migration.

Non-goals: hooks on skip (unchanged stacks never run hooks), hooks on self-heal
or on a rollback-only leg (neither is a git deploy), global (cross-stack) hooks,
hook ordering across stacks (`depends_on` already orders stacks), retry
policies, SyncFail-style compensation hooks, and automatic restore-on-rollback
(explicitly dropped — see Decisions #2).

See ADR-0037 for the accepted decision record.

## User model

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

- Each entry is a shell command line run via `sh -c` **through
  `command.Runner`** — so hooks get the log pipeline (ADR-0013), the timeout
  budget, and the recording fakes in tests, but keep shell ergonomics (pipes,
  redirects, `$(…)`, `>`) that an argv array would kill. This is the one
  deliberate `sh -c` in the codebase; it is documented as such next to the
  config reference.
- Hook environment = the stack's deploy env (Invariant 6 precedence:
  `env_files` > `vars_file` > `os.Environ()`), plus two injected variables so a
  single shared backup script can branch on context:
  - `SKIPPER_STACK` — the stack name.
  - `SKIPPER_HOOK` — `pre_deploy` or `post_deploy`.
- Working directory = the stack's effective project directory
  (`--project-directory` when set, else the compose file's dir — the same
  `stackRun.effectiveProjectDir()` every compose call uses).
- Hooks run **only when the stack actually deploys** (changed hashes → rollout),
  in list order, sequentially. A skipped (unchanged) stack, a self-heal `up`,
  and the restore leg of a rollback all run **no** hooks.

## Semantics

Exact position in the per-stack deploy sequence (from `deployStackIfChanged`):

```
pre_deploy hooks
  → pull (if images changed)
  → build (if Dockerfiles)
  → up -d [--wait --wait-timeout]        ── health gate 1
  → HTTP probe (if health_check.url)     ── health gate 2
  → post_deploy hooks
  → stop on-demand containers
  → record hashes/images + save state
```

`pre_deploy` runs first, *before* any container is touched — this is what makes
it a valid backup point: for an image bump the old container is still up, so
`docker exec … pg_dump` hits the running old version. `post_deploy` runs after
both health gates pass but before on-demand containers are stopped, so a smoke
test can still reach a service that is about to be stopped.

- **pre_deploy failure** → abort this stack's deploy before anything changed:
  `failed` event (the error names the failing hook and its index), hashes not
  saved, **no rollback** (nothing was touched, so there is nothing to roll
  back), next sync retries. Other stacks are unaffected except `depends_on`
  dependents, which defer exactly as on any failed dependency. Critically for
  the backup case: **if the backup fails, the update never starts.**
- **post_deploy failure** → enters the **existing rollback path** (Invariant 3
  / ADR-0022), treated identically to a health-check probe failure. See
  Decisions #1 for why this holds even without a `health_check`. With the old
  version restored, post hooks are **not** re-run against it (they validated the
  new version; the health gate re-runs as today when `health_check` is set).
  Event status: `rolled_back` / `rolled_back_unhealthy` via the same sentinel
  errors — no new statuses.
- Hooks are **not** hashed inputs (Invariant 2 untouched): editing a hook line
  in the stack config does not trigger a redeploy — it takes effect on the
  stack's next deploy. A hook that runs a script *from the repo* only triggers
  redeploys if that script sits under a `watch_dirs` entry; this footgun is
  documented next to the config reference.

## Failure containment

- A hanging hook is killed by its timeout: `hooks.timeout_seconds` bounds each
  hook downward, and the global `command_timeout_seconds` is the hard per-command
  ceiling (hooks go through the same runner as docker/git). `timeout_seconds: 0`
  (the default) means "bounded only by the global"; a value larger than the
  global has no extra effect. A timeout counts as a failure of that hook.
- Hooks run under the deploy mutex like everything else in a deploy — a slow
  hook delays the sync, same as a slow `pull`. That is the accepted cost; the
  timeout bounds it. A backup slower than `command_timeout_seconds` needs that
  global raised (it already bounds `pull`/`up`).
- Rollback of a *pre-failed* deploy is explicitly not a thing: state and
  containers were never touched.

## Package layout

- `internal/config`: a `Hooks` struct on `Stack` (`PreDeploy`, `PostDeploy`
  `[]string`; `TimeoutSeconds int`). Validation at load: no empty/whitespace
  command strings, `timeout_seconds >= 0`. In stack-discovery mode `hooks`
  lives in the repo `skipper.yaml` alongside the other deploy-shaping fields;
  like `env_files`/`watch_dirs` it participates in `Stack.ConfigHash` **only**
  in the sense already defined by Invariant 2 — but since editing a hook must
  *not* redeploy (above), `hooks` is excluded from `ConfigHash`, joining
  `icon`/`self_heal`/`depends_on` on that exclusion list.
- `internal/deploy/hooks.go`: `runHooks(ctx, run stackRun, phase string, cmds
  []string) error` — called at the two points in the sequence (timeout is read
  from `run.stack.Hooks`). No new package; hooks are meaningless outside a
  deploy. It builds the env via a shared `stackEnv` helper (base env + env_files
  + `SKIPPER_STACK` + `SKIPPER_HOOK`; `runDockerCompose` reuses the same helper)
  and shells each command via `runner.Run(ctx, run.effectiveProjectDir(), env,
  "sh", "-c", cmd)`, stopping at the first non-nil error and wrapping it with the
  phase and command index.

## Testing

- Table tests with the recording Runner: exact `sh -c` argv, env contents
  (`SKIPPER_STACK`, `SKIPPER_HOOK`, and Invariant-6 precedence), list order, and
  sequencing relative to pull/build/up/probe/on-demand-stop.
- pre-failure: compose is never invoked, no state save, `failed` event, the
  error names the hook index; a `depends_on` dependent blocks.
- post-failure: the rollback argv sequence is identical to a probe failure;
  `errors.Is(err, ErrRolledBack)` drives the event and post hooks are absent
  from the restore leg. Cover both `health_check` set (rollback re-runs the
  `--wait` gate → `rolled_back_unhealthy` possible) and unset (no-`--wait`
  restore leg).
- skip / self-heal / rollback-restore run zero hooks (guards the non-goals).
- timeout path: a hook that exceeds `timeout_seconds` is killed and counts as a
  failure (safety-critical failure path per the repo's coverage principle).

## Decisions

1. **post_deploy failure rolls back even without `health_check`.** A
   post_deploy hook the user wrote *is* a health gate — the whole point of
   `curl -fsS …/health` after `up` is "fail the deploy if this fails." Making
   rollback conditional on an unrelated `health_check` block would give post
   hooks two different meanings for no benefit and surprise the user who wrote a
   validation expecting it to matter. So: any post-`up` failure (probe *or*
   post_deploy hook) takes the rollback path; without `health_check` the restore
   `up` uses the no-`--wait` leg exactly as a probe failure does today. One
   mental model: *everything after `up` that fails, rolls back.*

2. **No automatic restore-on-rollback (no `on_rollback` hooks).** An
   `on_rollback` hook that e.g. `psql < /backup/paperless-….sql` was considered
   and **dropped, not deferred**: auto-restoring a database right after a failed
   deploy clobbers live data before anyone has inspected what broke. A
   rolled-back deploy already emits `rolled_back` / `rolled_back_unhealthy` and
   fires the existing alert (ADR-0020 / ADR-0031) — that alert is the entire
   "you need to act" signal. The operator restores by hand from the `pre_deploy`
   dump. No `on_rollback` hook is planned.

## Open questions

None outstanding — both original open questions are resolved in Decisions.
