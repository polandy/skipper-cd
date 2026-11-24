# ADR-0072: skipper owns the readiness wait and rolls back only on a decided failure

Status: accepted
Date: 2026-11-20

## Context

The deploy health gate (ADR-0022, ADR-0046) ran `docker compose up --wait
--wait-timeout <timeout_seconds>`, with a fixed 60s default. Compose's `--wait`
fails the same way whether a container turned `unhealthy`, exited, or simply
had not finished starting when the timeout ran out. skipper rolled back on all
three.

The third case is not a failure of the new version. A healthcheck with
`interval: 60s` cannot report healthy before its first probe at 60s, so the
default budget raced the first probe. A slow migration on start looked the
same. Each such race rolled back a working version, and on a stateful stack
the old image then ran against migrated data. The restored version could be
"unhealthy" for the same environmental reason (a dead database), which ended
in `rolled_back_unhealthy` with neither version running.

## Decision

**skipper polls the containers itself.** After a plain `up -d`, `awaitReady`
reads `docker compose ps --all` every `RolloutPollInterval` against the
injectable clock, until one of:

- **ready**: every container is `running` and, if it has a healthcheck,
  `healthy`. A container that exited 0 is a finished one-shot and counts as
  ready.
- **decided failure**: a container is `unhealthy`, exited/dead with a
  non-zero code, or restarting after one (a crash loop). This goes through
  the usual rollback path.
- **undecided**: the budget runs out while a container is still starting
  (`errStillStarting`). The deploy is `failed` and held (it carries
  `errNewVersionMark`) and is **never rolled back**. The message says
  "not rolled back" and names `POST /api/stacks/<stack>/retry`.

**The budget comes from the healthcheck.** It is
`start_period + (retries+1)·interval + timeout` of the slowest service, with
Docker's defaults for omitted or unreadable fields (30s interval, 30s
timeout, 3 retries); an uninterpolated `${VAR}` never fails the parse. It
is never below 60s. An explicit `timeout_seconds` replaces it. The derived
value is also the HTTP probe deadline and the canary default of a rollout.

**Roll forward once.** If the rollback ends in a decided failure of the
restored version too, the cause is most likely the environment and not the
change. skipper then applies the new version again with the full gate
(readiness, probe, `post_deploy` hooks):

- If that passes, the deploy is a plain `success`. The detour is logged at
  Warn only.
- If it fails, the result stays `rolled_back_unhealthy` and held. The error
  names both failures.

There is no roll-forward when the restored version was itself undecided
(still starting), and none with `rollback: false`: such a stack is never
restored, so it is held and notified as before.

**ConfigHash stays stable.** `Load` no longer defaults `timeout_seconds` to
60, but `ConfigHash` still hashes an omitted value as 60, so the upgrade
redeploys nothing.

The startup warning that compared `timeout_seconds` with
`command_timeout_seconds` is removed. The wait is no longer a single command
running under that ceiling.

## Rejected

- **Keep `compose --wait`, raise the default timeout.** Compose still cannot
  tell "starting" from "unhealthy", so a slow start still rolls back, only
  later. A larger fixed default also slows every legitimate rollback.
- **Treat an expiry as success.** An unverified version would record its
  hashes, and nothing would ever report it again.
- **Roll back an undecided expiry, like before.** That is the race this ADR
  removes. Holding keeps the evidence running, and a retry releases it.
- **Roll forward without limit, or with `rollback: false`.** One attempt
  covers a transient environment fault. Repeating it would loop a broken
  change, which the hold (ADR-0062) exists to stop.
- **A new event status for a roll-forward success.** The stack ends up on
  the version it was asked to run, and it is healthy. The notification and
  the history would only add noise.

## Consequences

- A gated deploy now waits as long as the healthcheck needs. With
  `interval: 60s`, that is several minutes before an undecided outcome.
  `start_interval` keeps this short (user docs).
- One more `ps` call per poll. Tests drive the wait through `Config.Now`, with
  no sleeps.
- The Go e2e stub's empty `ps --all` output counts as ready. The UI e2e for
  `rolled_back_unhealthy` now fails the roll-forward `up` as well.

## Amendment 2026-11-24: timeouts below the budget, and the undecided canary

An explicit `deploy_health_check.timeout_seconds` or
`rollout.health_timeout_seconds` below the derived budget turned a slow start
into a held failure, or, for a canary, into a removed container reported as
`rolled_back`, with nothing pointing at the timeout.

- **Explicit still wins, but never silently.** Each run logs a warning per
  stack, once per distinct finding, naming the key, the value, the budget and
  what to set. The still-starting error carries the same text. The check runs
  in the deploy run, not in config validation, so it covers both discovery and
  a static `stacks:` list.
- **The canary wait decides like `awaitReady`.** It runs on the injected clock.
  An unhealthy or non-zero-exited canary fails at once and is removed
  (`rolled_back`). A canary still starting at the timeout wraps
  `errStillStarting`: it is **kept** beside the old container, and the deploy is
  `failed` and held. Without a gate timeout and without
  `rollout.health_timeout_seconds`, the canary waits the budget, not a fixed 60s.

Rejected:

- **Raise the effective timeout to max(explicit, budget).** The budget is the
  worst case to a decided outcome, not the time to healthy. An operator who
  knows a service starts fast may keep a short wait, and the hold already keeps
  the containers running.
- **Remove a still-starting canary.** It kills a version that may only be slow
  and reports a rollback that never happened. Keeping it costs nothing in
  serving: the proxy only routes to it once healthy, and the old container
  stays. The price is two versions serving side by side until a retry or a new
  push completes the cutover.
