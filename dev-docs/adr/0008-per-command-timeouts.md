# ADR-0008: Timeouts apply per command, not per deploy run

Status: accepted
Date: 2026-07-09

## Context

`command_timeout_seconds` was documented as the limit for a single shell
command, but the webhook handler applied it as a deadline over the whole
sync-and-deploy run. With many stacks, or a slow `nixos-rebuild`, the
default 300s killed healthy runs mid-deploy. A run-wide deadline also grows
with the number of stacks, so no single value fits.

## Decision

The timeout lives in `command.ShellRunner`: every `Run`/`Output` call gets
its own `context.WithTimeout`. Constructors (`git.NewRepoSync`,
`git.NewRepoReader`, `deploy.NewDeployerWithCommitReader`) receive the
configured timeout and build their runner with it. Deploy runs themselves
have no overall deadline — the total is naturally bounded by
(number of commands × per-command timeout).

## Consequences

- A hung docker/git/nixos command is killed after `command_timeout_seconds`
  regardless of how long the rest of the run took; healthy long runs are no
  longer killed.
- The config documentation is accurate again.
- There is no single knob to cap a whole run; if that is ever needed it must
  be added explicitly.
