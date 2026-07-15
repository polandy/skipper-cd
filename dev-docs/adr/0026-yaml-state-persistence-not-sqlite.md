# ADR-0026: Persist state as YAML files, not an embedded database

Status: accepted
Date: 2026-07-15

## Context

skipper persists two things to `stateDir` (`/var/lib/skipper`):

- **`state.yaml`** — the current deploy truth: per-stack tracked-file hashes,
  per-stack image references, `last_deployed_commit`, and the transient
  `nixos_rebuild_in_flight` marker ([ADR-0025](0025-reconcile-self-restart-interrupted-nixos-rebuild.md)).
  A few KB for a couple dozen stacks.
- **`deploy-history.yaml`** — a bounded ring buffer of the last 100 deploy
  events, rendered for the web UI.

Both are read whole at startup, mutated in memory, and written whole via a
temp-file + `rename` (atomic; survives a `nixos-rebuild` that kills the process
mid-write). All deploys serialize on one mutex, so there is a single writer.

The question periodically comes up: should this move to SQLite (or another
embedded database) instead of YAML files?

## Decision

Keep the YAML files. Do not introduce SQLite or any embedded database for
persisted state.

Note this is only about *persisted state*. The user-facing config (`skipper.yml`)
is hand-authored / nix-rendered input and is YAML for the same readability
reasons — a database is a non-starter there.

## Consequences / Rationale

The properties a database buys do not match skipper's data:

- **The data is tiny and read/written whole.** No partial updates, no indexed
  lookups, no range or relational queries — exactly the workloads SQLite is for.
  A map load + in-memory mutate + full rewrite is simpler and already correct.
- **There is one writer.** Deploys serialize on a mutex, so transactions and
  row-level locking solve a concurrency problem that does not exist.
- **Crash-safety is already handled.** Temp-file + `rename` is atomic — the exact
  guarantee needed when the rebuild's switch restarts skipper mid-write. A
  database's WAL would re-solve a solved problem.
- **State stays human-inspectable and -editable.** Operational recovery leans on
  this: zeroing a stale hash to force a redeploy, reading `deploy-history.yaml`
  to see the last outcome, inspecting the in-flight marker — all done with
  `grep`/`sed`/`cat`. A binary DB would make every such intervention harder.
- **skipper stays dependency-light.** A SQLite driver is either cgo
  (`mattn/go-sqlite3` — breaks the clean cross-compile to argoneon's aarch64 and
  complicates the nix flake / vendoring) or a large pure-Go tree
  (`modernc.org/sqlite`). That is a lot of weight for KB-sized data, against the
  project's small-and-readable bias.

## Alternatives considered

- **SQLite / embedded DB.** Rejected as above: it solves problems skipper does
  not have and costs a heavy dependency plus state opacity.
- **Append-only JSONL for deploy history.** The only realistic future trigger is
  deploy history growing into an *unbounded, queryable audit log* (e.g. filtering
  or aggregating failures over months). It is bounded at 100 today. If that
  requirement ever appears, append-only JSONL — cheap appends, still greppable,
  no dependency — is the first step to reach for, ahead of SQLite. Time-series
  metrics are already covered by Prometheus, not this store.
