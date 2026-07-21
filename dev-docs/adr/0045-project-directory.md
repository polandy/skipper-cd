# ADR-0045: `project_directory` — rename `working_dir`, add a derived base directory

Status: accepted
Date: 2026-07-21

## Context

`working_dir` only ever does one thing: it is passed as `--project-directory`
to `docker compose`, fixing Docker Compose's project identity (container
labels, network/volume naming) and where `.env` is loaded from. Change
detection and the compose file always come from `stacks_base_dir/<name>`
regardless of this setting (Invariant 1). The name "working directory"
suggests "where work happens" — the opposite of the truth for this field,
since it explicitly does *not* determine where the compose file lives or
where hashing happens. That gap between name and behavior is significant
enough that it is called out as an explicit "never conflate the two" warning
in CLAUDE.md's invariants — a name that needs a standing doc warning to avoid
being misread is a candidate for a better name. `project_directory` names
exactly what it sets: the flag it becomes on the command line.

Separately: `working_dir` is only needed when a stack is *also* managed
outside skipper — typically a NixOS systemd service pointing
`WorkingDirectory` at `/etc/nixos/modules/<name>` (docs/nixos.md), so that a
manual `docker compose up -d` run by hand in that directory (for local
testing, or the systemd unit itself) resolves to the *same* compose project
skipper manages — same container/network/volume names — rather than a second,
parallel project. In that setup every stack on the host follows the same
`<base>/<name>` pattern, yet each one had to repeat the full path in its own
`stacks:` entry (or, with the self-registering NixOS pattern in docs/nixos.md,
in its own service module) — config that carries no information beyond the
stack's own name. `stacks_base_dir` already solves the equivalent problem for
the compose path, but it cannot also serve this: the two directories are
conceptually and often physically different (the deploy-repo clone vs. a
NixOS modules directory) and Invariant 1 requires they never be conflated.

## Decision

**Rename `working_dir` to `project_directory`** (`Stack.WorkingDir` →
`Stack.ProjectDirectory`, YAML key `working_dir` → `project_directory`).
Clean break, no alias — consistent with ADR-0043's precedent (the only users
are two single-admin homelab hosts, migrated atomically alongside a host
config edit + rebuild). The Go-internal `stackRun.projectDir` field this
value ultimately feeds was already named well; only the config-facing name
was wrong.

**Add an optional top-level `project_directory_base` config field.** When a
stack sets no `project_directory` of its own, it defaults to
`<project_directory_base>/<name>` — mirroring `stacks_base_dir`'s role for the
compose path, but as a separate field so the two bases can differ. An
explicit per-stack `project_directory` always wins; `project_directory_base`
only fills the gap when the stack sets nothing. Applied in both config modes:
`Load` for the direct `stacks:` list (the non-discovery stack set), and
`LoadRepoStacks` for the discovered set under stack discovery (ADR-0034/0043),
so a discovered stack with no override entry still gets the derived value.
Like `project_directory` itself, `project_directory_base` must be an absolute
path — checked at startup.

`project_directory` remains part of a stack's `ConfigHash`
(`stackDeployInputs`, ADR-0043), so an edit to `project_directory_base` still
redeploys exactly the stacks whose derived path changes — the same behavior
an explicit `project_directory` edit already had. Because the YAML key
renamed, the canonical hash input changes for every stack regardless of value
— see Consequences.

## Consequences

- The config field name now says what it does; CLAUDE.md's invariant no
  longer needs a "never conflate the two" warning to stay correctly used —
  though the warning stays, since the underlying distinction (compose-file
  source vs. `--project-directory`) is still worth stating plainly.
- **One-time redeploy of every stack on upgrade.** `stackDeployInputs`'
  `yaml:"working_dir"` tag becoming `yaml:"project_directory"` changes the
  canonically-marshaled hash input even for a stack whose value didn't
  change, so `Stack.ConfigHash` changes for every discovered stack on the
  first sync after upgrading. A one-time `docker compose up -d` per stack is
  the entire effect (idempotent, no downtime for an unchanged deploy) —
  accepted, not worth a migration shim for two single-admin hosts.
- **Migration required on both homelab hosts**: every `working_dir:` /
  `working_dir = "...";` in host-a's and host-b's NixOS config (host config and
  any self-registering stack modules) must become `project_directory` before
  or alongside the upgrade, or that stack silently loses its
  `--project-directory` override (falls back to the compose file's own
  directory) rather than erroring — YAML/Nix have no way to flag an unknown
  field as fatal here the way `LoadRepoStacks`' strict decoding does for the
  in-repo override file.
- A NixOS homelab host with all stacks under one systemd-managed modules
  directory sets `project_directory_base` once instead of a
  `project_directory` line per stack. A stack whose directory breaks the
  pattern still overrides it explicitly.
- No new invariant: `project_directory_base` is source material for deriving
  `project_directory`, which keeps its existing role (`--project-directory`
  only, never conflated with the compose-file source, Invariant 1).
