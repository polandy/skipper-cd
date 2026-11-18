# ADR-0002: Hash-based change detection with persisted state

Status: accepted
Date: 2026-07-09 (documents a decision made at project start)

## Context

On every push webhook skipper-cd must decide which Docker Compose stacks to
redeploy. Alternatives considered: git diff between commits (fragile across
force-pushes, misses local file changes such as env files outside the repo),
timestamps (unreliable across clones/resets), or always redeploying
everything (slow, causes restarts of unchanged services).

## Decision

Each stack's tracked inputs — compose file, `env_files`, global `vars_file`,
`watch_dirs` contents, Dockerfiles of `build:` services — are hashed with
SHA-256. Hashes are persisted per stack in `state.yaml`. A stack redeploys
iff any hash differs. Change detection always reads from the repo clone
(`stacks_base_dir/<name>/docker-compose.yml`), never from `working_dir`.

A missing or corrupt `state.yaml` means **all stacks redeploy**. This is by
design: redeploying an unchanged stack is idempotent and safe, whereas
skipping a changed one is not.

## Consequences

- Deploys are independent of git history; force-pushes and out-of-repo env
  files are handled correctly.
- State loss degrades to a slow-but-correct full redeploy.
- Anything that influences a deployment must be added to the hashed input
  set (CLAUDE.md invariant 2), otherwise changes go unnoticed.
