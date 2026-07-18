# ADR-0034: Stack definitions come from the deploy repo (auto-discovery + central overrides)

Status: accepted
Date: 2026-07-18

## Context

Before this decision the set of stacks — and all per-stack config
(`env_files`, `health_check`, `depends_on`, …) — lived in the host
`skipper.yml`, which in the homelab is rendered from `/etc/nixos`. The deploy
repo was the source of truth for stack *content*, but not for stack
*membership*. Consequences:

- Adding a stack took a repo push **plus** an `/etc/nixos` edit and a
  rebuild — two systems for one logical change.
- skipper could not know which running compose projects it is supposed to
  own, leaving future orphan detection/prune to rest on heuristics instead
  of a declared set.
- ArgoCD, the model skipper mirrors for docker-compose, keeps application
  membership *and* spec in git; the cluster host holds only bootstrap config.

## Options considered

**0. Status quo** — stacks stay in the host config. Rejected: keeps the
two-system change and leaves orphan detection heuristic.

**Per-stack config files** (`stack.yml` next to each compose file,
ArgoCD-style app manifests). Rejected: configuration scattered across many
small files; no single place that describes the host.

**A. Auto-discovery + central overrides file.** Every direct subdirectory of
`stacks_base_dir` containing a `docker-compose.yml` *is* a stack (name =
directory name, defaults apply). An optional `skipper.yaml` at the root of
`stacks_base_dir` (see the 2026-07-18 amendment) holds only the exceptions:
`depends_on`, `env_files`, `health_check`,
`disabled: true`, … Membership truth is the directory tree.

- Strongest argument for: the explicit-list failure mode disappears — with a
  list, pushing a new stack directory but forgetting the list entry deploys
  nothing, silently; exactly the kind of quiet drift skipper otherwise
  eliminates.
- Cost: a directory cannot exist without being deployed; WIP/parked stacks
  need an explicit opt-out (`disabled: true`).

**B. Explicit central list + undeclared-directory warning.** The repo-root
`skipper.yaml` lists every stack (source of truth); discovery runs only to
*warn* about directories not in the list. One complete file describes the
host; undeclared WIP directories are legitimate.

- Strongest argument for: explicitness — the file is the readable inventory,
  and "in the repo but not deployed" needs no marker.
- Cost: every stack costs a list entry even when it needs zero config; the
  forgotten-entry case is only a warning, not self-healing.

## Decision

**Variant A**: stacks are auto-discovered from the repo; a `skipper.yaml` at
the `stacks_base_dir` root carries only per-stack overrides and the `disabled: true`
opt-out. Decided 2026-07-18 — marking the exception ("in the repo but must
not run") is more honest than repeating the rule ("in the repo, should run")
for every stack, and the silent-miss failure mode of a list outweighs its
readability edge, since `ls` on `stacks_base_dir` and the UI deploy table
provide the inventory.

The host `skipper.yml` keeps host concerns only and opts in via
`stack_discovery: true` (mutually exclusive with a `stacks:` list; legacy
mode is unchanged). Design details in `dev-docs/stack-discovery-spec.md`.

## Consequences

- One git push adds, changes, or removes a stack — no `/etc/nixos` edit, no
  rebuild. Membership and per-stack config are versioned, diffable, and
  arrive atomically with the content they describe.
- Stack-config validation moves from skipper startup (and the nix eval
  assertions) to sync time. Containment rule: an unparseable `skipper.yaml`
  fails the sync's stack phase loudly (reserved `_config` event key, nothing
  deploys; the nixos phase is host-config-driven and unaffected); *semantic*
  errors (unknown override entry, broken `depends_on` edge, cycle, invalid
  field) fail only the affected stacks — their dependents `block`, the rest
  deploy. Errors with a known file location carry a `>`-marked excerpt of
  the offending `skipper.yaml` lines in their message, so the failed row's
  error panel shows the config, not just its name.
- The per-stack effective config becomes a hashed input (invariant 2 grows
  one entry), keyed by the `skipper.yaml` path (at `stacks_base_dir`) so the UI
  attributes the change to that file and shows its real git diff. Only deploy-shaping
  fields are hashed (`working_dir`, `env_files`, `watch_dirs`,
  `on_demand_containers`, `health_check`) — display-only (`icon`),
  runtime-only (`self_heal`), and ordering-only (`depends_on`) fields never
  redeploy. Enabling discovery redeploys every stack once (new hash input);
  accepted.
- Two deliberate v1 limitations: a per-stack `autosync` override is not
  available in discovery mode (the autosync controller's config baseline is
  fixed at startup; global autosync + UI overrides work), and self-heal
  *activation* follows the global flag alone (the stack set is unknown at
  startup) — per-stack `self_heal: false` still opts out.
- Orphan detection gets its authoritative managed set: discovered = managed
  (see `dev-docs/orphan-detection-spec.md`).
- The NixOS module's per-stack options are legacy-mode-only under discovery;
  the module keeps rendering host-level config.
- skipper's trust boundary shifts: a repo push can now change health gates,
  env wiring, and ordering — acceptable for the single-admin homelab this
  targets, and no different from ArgoCD's model.

## Amendment (2026-07-18): override file lives at `stacks_base_dir`, not the repo root

The override file is read from `<stacks_base_dir>/skipper.yaml`, not the repo
root as originally shipped. Relative `env_files`/`watch_dirs` paths resolve
against `stacks_base_dir` too, next to the stacks they configure.

Motivation: one deploy repo can serve several hosts that each watch a
*different* subtree (in the homelab, `/etc/nixos` is the deploy repo; the main
host watches `modules/`, a second host watches `system/<host>/modules/`). A
single repo-root `skipper.yaml` is shared by all of them, and an override entry
for a stack a host does not discover is an entry-level error ("no stack
directory …"). Anchoring the file at `stacks_base_dir` gives each watched
subtree its own `skipper.yaml`, so the per-host stack sets — and their
overrides — stay disjoint. Discovery is not yet enabled anywhere, so there is
no compatibility cost.
