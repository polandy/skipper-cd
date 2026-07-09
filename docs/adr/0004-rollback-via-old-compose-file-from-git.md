# ADR-0004: Rollback via the old compose file from git

Status: accepted
Date: 2026-07-09 (documents an existing decision; sentinel error added
2026-07-09)

## Context

When `docker compose up` fails mid-deploy, containers can be left in a
broken state. Alternatives: no rollback (leave broken), docker image tag
snapshots (heavy, doesn't cover config changes), or re-applying the previous
compose file.

## Decision

On a failed `up`, the deployer fetches the compose file as of
`state.LastDeployedCommit` from git into a temp file and runs
`docker compose up` with it. `--project-directory` must then point at the
original compose directory, because the temp file lives in /tmp and project
identity/.env loading must not change.

A successful rollback still returns an error wrapping `deploy.ErrRolledBack`
(checked with `errors.Is`), which drives the `rolled_back` event status.
Only the compose file is rolled back — env files and the global vars file
are read in their current state.

## Consequences

- A broken deploy self-heals to the previously running version; the run
  still counts as failed so the state hashes are not updated and the next
  push retries.
- Rollback needs a previous commit; the very first deploy cannot roll back.
- Env-file-only breakage is not covered by rollback (inputs are current).
