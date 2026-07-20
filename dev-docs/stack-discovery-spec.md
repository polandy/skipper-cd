# Feature Spec: Stack Discovery from the Deploy Repo

Status: implemented (ADR-0034, variant A)
Date: 2026-07-18

## Goal

The deploy repo declares the host completely: every stack directory in the
repo *is* a deployed stack, per-stack config lives in **one** central
`skipper.yaml` at the `stacks_base_dir` root, and adding/changing/removing a stack is a
single git push. The host `skipper.yml` shrinks to host concerns. Decision
record incl. the rejected explicit-list variant: ADR-0034.

Non-goals: per-stack config files in stack directories (rejected — scattered
config), moving host concerns (repo URL, webhook, ports, notifications,
`nixos_rebuild`, reconcile interval, global policy defaults) into the repo,
multi-repo support.

## Discovery model

- A stack is every **direct** subdirectory of `stacks_base_dir` (inside the
  clone) that contains a `docker-compose.yml`. Nested and hidden directories
  are not scanned; directories without a compose file are ignored.
- Stack name = directory name; deploy order is alphabetical, refined by
  `depends_on` (the same stable topo sort as ADR-0032). Defaults apply — a
  bare directory with a compose file is a fully functional stack.
- The discovered set is computed on **every sync** from the clone at the
  synced commit (`config.LoadRepoStacks`), after the nixos phase and before
  change detection. New directory → new stack (no state entry → deploys).
- The deployer publishes the set (`Deployer.CurrentStacks`); the health
  poller, self-heal, the autosync/queue order, and the icon resolver read
  the effective stacks through it (`stacksNow` in `cmd/skipper/main.go`).

## Central overrides: `skipper.yaml` at `stacks_base_dir`

```yaml
# skipper.yaml — at the root of stacks_base_dir; optional, entries only for exceptions
stacks:
  traefik:
    depends_on: [authelia, crowdsec]
  paperless:
    env_files: [paperless/secrets.env]
    health_check: { timeout_seconds: 60 }
  experiments:
    disabled: true        # in the repo, deliberately not deployed
```

The file lives **at the root of the watched `stacks_base_dir`**, not the repo
root (ADR-0034 amendment, 2026-07-18): one deploy repo can serve several hosts
that each watch a different subtree, and each gets its own disjoint
`skipper.yaml` — an override for a stack the host does not discover would
otherwise be an entry-level error.

- Keyed by stack name; every key is optional. Available fields: the
  per-stack fields of the host config except `name` and `autosync`
  (`project_directory`, `env_files`, `watch_dirs`, `on_demand_containers`, `icon`,
  `health_check`, `self_heal`, `depends_on`) plus `disabled`. Relative
  `env_files`/`watch_dirs` paths resolve against `stacks_base_dir`. Decoding is
  strict — an unknown field is a file-level error, so a typo fails loudly
  instead of silently deploying without the setting.
- `disabled: true` — skipper ignores the stack entirely: not deployed, not
  pruned, not health-polled. A running stack that becomes disabled **keeps
  running** (skipper hands it off, it does not tear it down). A `depends_on`
  reference to a disabled stack is valid and reads as satisfied.
- An entry whose name matches no discovered directory is an entry-level
  error (typo/rename guard).

## Host config changes

- New host-level bool `stack_discovery: true` (default `false`) switches the
  instance to discovery mode. Setting it **and** a non-empty `stacks:` list
  is a startup config error — the two sources never merge. Host-list mode
  (an explicit `stacks:` list) keeps working unchanged; requires `stacks_base_dir`.
- NixOS module: the per-stack module options apply only in host-list mode; under
  discovery the module renders host-level config.

## Validation & failure containment (sync time)

Two severity levels, evaluated on every sync:

1. **File-level** — `skipper.yaml` unparseable (broken YAML, unknown field,
   wrong types) or `stacks_base_dir` unreadable: the sync's **stack phase
   aborts**: nothing deploys, running containers untouched. One `failed`
   event under the reserved key `_config` (analogous to `_nixos`) carries
   the error; the next sync/push retries. The nixos-rebuild phase is
   unaffected (its inputs are host-config-defined and it runs first).
2. **Entry-level** — unknown override name, reserved name (`_` prefix),
   invalid field value, unknown/self `depends_on` reference, dependency
   cycle: only the **affected stacks** fail (`failed` event naming the
   offense; cycle members all fail). They are seeded into the dependency
   gate as blocked, so their dependents get the existing `blocked` handling;
   every other stack deploys normally.

Every error with a known file location carries a numbered, `>`-marked
excerpt (±2 lines) of the repo `skipper.yaml` in its message: parse and
unknown-field errors take the line from the yaml.v3 error text; entry-level
errors are located via a `yaml.Node` line index — the entry key for unknown
entries, the `health_check`/`depends_on` field line for field errors and
cycle members. Errors with no location (e.g. a reserved directory name with
no `skipper.yaml` entry) stay clean. The excerpt travels in the plain error
string, so the UI error panel (monospace), notifications, and the audit log
carry it with no schema change.

## Change detection (invariant 2 grows one input)

- Each stack's **deploy-shaping** config — `project_directory`, `env_files`,
  `watch_dirs`, `on_demand_containers`, `health_check`, canonically
  marshaled — is hashed into `Stack.ConfigHash` and recorded under the repo
  `skipper.yaml` path in the stack's hash map. A config edit therefore
  redeploys exactly the affected stacks, and the UI attributes the change to
  `skipper.yaml` with its real git diff.
- Display-only (`icon`), runtime-only (`self_heal`), and ordering-only
  (`depends_on`) fields are deliberately excluded — editing them never
  redeploys (`icon` keeps its documented "never hashed" promise).
- This closes a today-gap: host-config edits (e.g. a changed `env_files`
  list) were invisible to change detection until file contents changed.
- Migration effect: enabling discovery introduces the new hash input →
  every stack redeploys once. Accepted and documented.

## v1 limitations (deliberate)

- **No per-stack `autosync` override** in the repo file: the autosync
  controller's config baseline is fixed at startup. Global autosync and the
  non-persistent UI overrides work as usual.
- **Self-heal activation follows the global flag** (`self_heal: true` in the
  host config): the stack set is unknown at startup, so a per-stack-only
  activation is not possible; per-stack `self_heal: false` still opts a
  stack out at heal time.
## UI surface

- **Disabled line** — the `stacks` SSE snapshot (`{"disabled": [...]}`,
  published on connect and after every deploy run) drives a quiet chip line
  below the deploy table (`data-testid="disabled-stacks"`); hidden when
  empty and in the logs view. Deliberately not table rows — the table is an
  event log, a disabled stack has no events.
- **`_config` row** — file-level failures render through the ordinary
  failed-event path (error panel shows the message incl. the marked
  excerpt); the pseudo-stack resolves the `git` icon, like `_nixos` resolves
  `nixos`. Details in `internal/ui/UI_SPEC.md`; e2e coverage is Maske O
  (`dev-docs/e2e-tests.md` §4.16).

## Interactions

- **Ordering (ADR-0032):** `depends_on` moves into the repo file; topo sort
  and `blocked` semantics unchanged. Rename + edge update are one atomic
  commit in one file.
- **Run plan / upcoming (ADR-0024)** and the queue/autosync snapshots run
  over the effective set.
- **Env precedence, compose-from-clone, rollback (invariants 1/3/6):**
  untouched — discovery changes where stack definitions come from, not what
  a deploy does.
- **Orphans/prune (proposal):** discovered = managed; see
  `orphan-detection-spec.md`.

## Package layout

- `internal/config/repo.go`: discovery + overrides + entry/file-level
  validation + `ConfigHash` — pure (fs in, stacks out), no deploy knowledge.
  `LoadRepoStacks(repoDir, stacksBaseDir) ([]Stack, []StackError, error)`;
  the `error` is file-level, `[]StackError` entry-level.
- `internal/deploy/discovery.go`: `ConfigStateKey`, `CurrentStacks`,
  effective-stack lookup for self-heal, and the config-hash injection into
  the per-stack hash maps. `DeployAllStacks` swaps in the discovered set and
  seeds entry-level failures into the dependency gate.
- `cmd/skipper/main.go`: `stacksNow` closure feeding every stack-enumerating
  consumer.
