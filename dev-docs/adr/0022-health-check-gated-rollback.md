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

**The rollback is verified through the same gate.** When `health_check` is
configured, the rollback `up` also runs with `--wait --wait-timeout`, and the
HTTP probe (if any) must pass again. `rolled_back` therefore guarantees the
old version is actually healthy again. If the restored version *also* fails
the gate — typically an environment problem (dead database, broken secret)
that no compose version fixes — the error wraps `ErrRollbackUnhealthy` and
the run emits **`rolled_back_unhealthy`**: the stack sits on the old compose
file but is not verified healthy, so it needs attention now. A rollback that
cannot even run (no previous commit, compose file not retrievable) stays a
plain `failed` with "rollback also failed" — nothing was restored. Without
`health_check`, the rollback `up` remains a plain `up -d`; the previous
behavior is unchanged.

## Consequences

- A deploy that starts but does not become healthy self-heals to the
  previous version instead of silently staying broken.
- `--wait` treats a service that exits as a failure; stacks with deliberate
  one-shot containers should not enable `health_check` (or model them with
  `service_completed_successfully` dependencies).
- With `health_check` configured, a failed deploy can wait twice (deploy gate
  plus rollback gate), doubling the worst-case deploy time. Accepted: an
  honest `rolled_back` vs `rolled_back_unhealthy` distinction is worth more
  than a faster but unverified "assumed good" rollback.
- `rolled_back_unhealthy` usually means the environment is broken, not the
  compose change — the next push retries the new version regardless, because
  state hashes were not recorded.
- `--wait-timeout` requires Docker Compose v2.17+.
- The HTTP probe runs from the skipper-cd host; the URL must be reachable
  from there (e.g. a published port on localhost). For containers whose
  endpoint is not exposed, stage 1 is the answer: the compose `healthcheck:`
  runs inside the container, so `--wait` gates on it without any published
  port. The probe is the optional extra for host-reachable endpoints, not
  the primary mechanism — the docs lead with the compose healthcheck.
