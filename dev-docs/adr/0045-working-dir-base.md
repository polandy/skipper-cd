# ADR-0045: `working_dir_base` — a derived default for per-stack `working_dir`

Status: accepted
Date: 2026-07-21

## Context

`working_dir` is only needed when a stack is *also* managed outside skipper —
typically a NixOS systemd service pointing `WorkingDirectory` at
`/etc/nixos/modules/<name>` (docs/nixos.md). In that setup every stack on the
host follows the same `<base>/<name>` pattern, yet each one had to repeat the
full path in its own `stacks:` entry (or, with the self-registering NixOS
pattern in docs/nixos.md, in its own service module) — config that carries no
information beyond the stack's own name.

`stacks_base_dir` already solves the equivalent problem for the compose path
(`<stacks_base_dir>/<name>/docker-compose.yml`, Invariant 1), but it cannot
also serve `working_dir`: the two directories are conceptually and often
physically different (the deploy-repo clone vs. a NixOS modules directory)
and Invariant 1 requires they never be conflated.

## Decision

Add an optional top-level `working_dir_base` config field. When a stack sets
no `working_dir` of its own, it defaults to `<working_dir_base>/<name>` —
mirroring `stacks_base_dir`'s role for the compose path, but as a separate
field so the two bases can differ. An explicit per-stack `working_dir` always
wins; `working_dir_base` only fills the gap when the stack sets nothing.
Applied in both config modes: `Load` for the direct `stacks:` list (the
non-discovery stack set), and `LoadRepoStacks` for the discovered set under
stack discovery (ADR-0034/0043), so a discovered stack with no override entry
still gets the derived `working_dir`. Like `working_dir` itself,
`working_dir_base` must be an absolute path — checked at startup.

`working_dir` remains part of a stack's `ConfigHash` (`stackDeployInputs`,
ADR-0043), so an edit to `working_dir_base` still redeploys exactly the
stacks whose derived path changes — the same behavior an explicit
`working_dir` edit already had.

## Consequences

- A NixOS homelab host with all stacks under one systemd-managed modules
  directory sets `working_dir_base` once instead of a `working_dir` line per
  stack. A stack whose directory breaks the pattern still overrides it
  explicitly.
- No behavior change for a config that never sets `working_dir_base`: the
  field defaults to empty, `working_dir` stays exactly what the stack sets
  (or unset).
- No new invariant: `working_dir_base` is source material for deriving
  `working_dir`, which keeps its existing role (`--project-directory` only,
  never conflated with the compose-file source, Invariant 1).
