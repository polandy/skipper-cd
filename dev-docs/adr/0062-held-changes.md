# ADR-0062: A change that failed after it started is held, not retried every tick

Date: 2026-09-29

## Status

Accepted

## Context

A deploy that fails leaves its stack's hashes unrecorded, so the change stays
pending and every later run deploys it again. The periodic reconcile runs every
five minutes. For a failure caused by the change itself, every retry fails the
same way.

In production a `signal-cli-rest-api` image bump did exactly that for five
hours. Every five minutes it went through the same cycle: a
`rolled_back_unhealthy`, three self-heal attempts, then `heal_exhausted`. That
is about 295 events. The history keeps 200 records per stack, so the loop
evicted the stack's whole history, including the deploy that started it.
ADR-0056 collapses a failure that repeats verbatim, but only when the repeats
are consecutive. A five-status cycle never is. Every retry also started with a
`deploying` event, which reset self-heal's attempt budget (ADR-0029), so the
cycle regenerated itself.

Retrying is right for some failures. A registry that is briefly unreachable, a
`pre_deploy` backup that times out, or a pull that hits a rate limit will
probably succeed on the next tick, and nobody wants to click through those.
What separates the two kinds is whether the new version ever ran.

Alternatives considered:

- **Exponential backoff for every failure.** This keeps the loop, only slower:
  a broken image still redeploys all night, just less often, and it still
  resets self-heal every time.
- **Holding every failure until a push.** This would strand a transient pull
  failure until someone touches the repo.
- **Folding the cycle in the UI only.** The events are still written, so the
  history is still evicted and the stack still flaps.

## Decision

A failure is classified by where it happened:

- **After the new version was applied.** This covers `up`, a health gate, a
  `post_deploy` hook, and a rollout cutover. Every such path goes through
  `rollBackFailedDeploy` or the rollout's canary path and is tagged
  `ErrNewVersionFailed`. This happens regardless of the rollback outcome, and
  with rollback disabled too. The change is **held**: `state.yaml` records
  `held.<stack>` with a fingerprint of the stack's tracked-file hashes, the
  time, the failure's status and its newest commit.
- **Before any container was touched.** This covers the hash computation, a
  `pre_deploy` hook, `pull` and `build`. The failure is not held, and the next
  tick retries it as before.

A run whose inputs for a held stack still match the fingerprint does not deploy
it. It emits a live-only `held` event instead. Like `skipped`, that event never
reaches the history, the audit log or a notification. The run returns `ErrHeld`,
so changed dependents are `blocked` as they would be behind a failure.

A hold is released by:

- inputs that no longer match the fingerprint, meaning a new push;
- an operator retry, `POST /api/stacks/{stack}/retry`, which is one attempt and
  holds the change again if it fails again;
- a revert back to the recorded inputs;
- removal of the stack.

The retry request lives in memory and is consumed by the next run. The endpoint
triggers that run itself. It answers 409 for a stack with nothing held.

The standing condition is the `skipper_stack_held` gauge together with a `held`
chip on the roster entry. The chip carries a retry button on the primary's own
rows only; peer rows are read-only. The failure event itself fires once, like a
standing config error (ADR-0055).

## Consequences

- A broken change produces one failure event and then silence until someone acts
  on it. The history keeps what happened before it.
- Self-heal is no longer reset by retries that do not happen. A retry that the
  operator asks for, or a new push, still grants a fresh budget.
- A failure caused by the environment rather than the change, such as a
  dependency that was down during the deploy, now needs a retry or a push
  instead of healing on its own. The retry button exists for that case.
- A previously held stack that is retried by the operator and fails again is
  reported again. That is one event per click, not per tick.
- Holds survive a restart: they are persisted, and a new deployer seeds its view
  from `state.yaml`. A pending retry request does not survive a restart; the
  operator clicks again.
