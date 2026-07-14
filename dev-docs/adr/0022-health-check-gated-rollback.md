# ADR-0022: Health-check-gated rollback

Status: accepted
Date: 2026-07-14

## Context

Rollback (ADR-0004) only fires when `docker compose up` itself fails. The
more common real-world failure is a deploy that *starts* fine but is broken:
the container crash-loops, or the app serves 500s. Those deploys were marked
`success` and stayed broken until the next push.

Alternatives considered: polling `docker compose ps --format json` for
health (needs output-parsing through the Runner abstraction, reimplements
what compose already does), or an external monitoring hook (out of scope —
skipper-cd stays self-contained).

## Decision

An optional per-stack `health_check` section gates the deploy in two stages:

1. **Compose health**: when the section is present, `up` runs with
   `--wait --wait-timeout <timeout_seconds>`. Compose itself then waits for
   every service to be `running` (no healthcheck) or `healthy` (with one)
   and exits non-zero otherwise — which lands in the *existing* rollback
   path with no new failure-handling code.
2. **HTTP probe** (optional `url`): after a successful up, the deployer
   GET-polls the URL every 2 s until it answers 2xx; if it never does within
   `timeout_seconds`, the stack is rolled back the same way.

Both stages share one `timeout_seconds` (default 60). The probe uses a
consumer-side `httpDoer` interface so tests inject a fake client, mirroring
the Runner pattern (ADR-0003). A health-gated failure behaves exactly like a
failed up: state hashes are not recorded (the next sync retries), the error
wraps `ErrRolledBack`, and the run emits `rolled_back` — so events, metrics
(`skipper_deploy_rollbacks_total`) and notifications (ADR-0020) all work
unchanged.

## Consequences

- A deploy that starts but does not become healthy self-heals to the
  previous version instead of silently staying broken.
- `--wait` treats a service that exits as a failure; stacks with deliberate
  one-shot containers should not enable `health_check` (or model them with
  `service_completed_successfully` dependencies).
- The rollback itself runs plain `up -d` without `--wait`: the old version
  is assumed good, and a second wait could double the worst-case deploy time.
- `--wait-timeout` requires Docker Compose v2.17+.
- The HTTP probe runs from the skipper-cd host; the URL must be reachable
  from there (e.g. a published port on localhost).
