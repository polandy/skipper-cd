# ADR-0007: Graceful shutdown waits for the running deploy

Status: accepted
Date: 2026-07-09

## Context

A service restart (systemd, new release) could previously kill the process
in the middle of `docker compose up`, leaving a stack half-deployed and its
state hashes unwritten.

## Decision

On SIGTERM/SIGINT the process first shuts the webhook HTTP server down
gracefully (bounded by a 10s timeout, so open SSE streams cannot block
shutdown), then blocks on `Deployer.WaitIdle()` — a lock/unlock barrier on
the deploy mutex — until an in-flight deploy run has finished, and only then
exits. Both HTTP servers use `ReadHeaderTimeout` against slowloris
connections.

## Consequences

- Restarts no longer interrupt docker compose mid-run; shutdown takes as
  long as the current deploy (per-command timeouts bound this, ADR-0008).
- Deploys queued but not yet started at shutdown are dropped; the startup
  sync-and-deploy run catches their changes up.
- Supervisors need a stop timeout larger than a typical deploy (systemd
  `TimeoutStopSec`).
