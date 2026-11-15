# ADR-0064: A failure before start backs off, then is held

## Status

Accepted. Amends [ADR-0062](0062-held-changes.md).

## Context

ADR-0062 holds a change whose new version failed after it started. A failure
*before* any container was touched (a `pre_deploy` hook, `pull`, `build`) was
left out on purpose and retried on every reconcile tick. The reasoning was that
such failures are usually transient: a registry that is briefly unreachable, a
rate limit, a backup that timed out.

A deterministic failure takes the same path. In production a Renovate digest
bump of the Nextcloud base image met a Dockerfile that pinned a Debian package
version the mirror no longer shipped. From 02:06 to 07:20 skipper rebuilt the
change on every tick: 62 identical failures, each running a full
`apt-get update` against the Debian mirrors, each sending a notification. That
is the loop ADR-0062 set out to end, entered through the door it left open.

ADR-0062 rejected exponential backoff for failures after start, for two
reasons: the loop goes on, only slower, and every retry starts with a
`deploying` event that resets self-heal's attempt budget, so a broken stack
keeps flapping. Neither reason applies before start:

- Nothing was touched. The previous version keeps running, healthy as before,
  so there is no flapping for a retry to feed and no self-heal budget being
  spent. A `deploying` event still resets the budget, but for a stack self-heal
  has no reason to act on.
- The loop does not go on: the backoff here is bounded by a hold, so it ends
  after a fixed number of attempts instead of slowing down forever.

## Decision

A failure before start (any deploy error that does not carry
`ErrNewVersionFailed`) is recorded in the stack's `held` entry, the same record
ADR-0062 uses, with two extra fields:

- `attempts`: consecutive failures before start with the same input
  fingerprint. A failure with other inputs starts again at 1.
- `retry_at`: when the change may be attempted again. It is the attempt's start
  plus 5 minutes after the first failure and plus 10 after the second. The
  third failure leaves it empty, which makes the record an ordinary hold.

While `retry_at` lies in the future, a run treats the stack exactly like a held
one: a live-only `held` event, `ErrHeld`, and changed dependents `blocked`. A
run that reaches the stack within a minute of `retry_at` attempts it. Ticks are
as far apart as the first wait, and the stack's turn within a run shifts by
seconds from run to run, so without that tolerance the first retry would land
one or two ticks later by chance.

Everything else is ADR-0062 unchanged:

- A new push, a revert, or removing the stack releases the record.
- The operator retry attempts the change at once and counts as one attempt.
- A successful deploy clears the record.
- The record lives in `state.yaml`, so it survives a restart, and a restarted
  skipper neither retries early nor forgets the count.

Two cases are deliberately not counted:

- A deploy cut short by shutdown (`ctx` cancelled) says nothing about the change.
- A hashing failure has no fingerprint to key on and still retries every tick,
  as before.

The `skipper_stack_held` gauge is raised only for a hold without `retry_at`. A
backoff ends on its own, and every attempt is already counted in
`skipper_deploy_errors_total`. In the UI the backoff wears the hold's chip,
reading `backoff` with the next attempt in its tooltip.

The values are constants, not configuration. Three attempts over about fifteen
minutes outlast a registry hiccup or a rate-limit window, and an operator who
knows the cause is gone has the retry button. A separate cap on the wait is not
needed, because the hold ends the series before a cap could apply.

## Consequences

- A deterministic build failure costs three attempts and three notifications,
  then silence until someone acts. It no longer costs one attempt per tick all
  night.
- A transient failure that heals within the backoff deploys on its own as
  before, only up to fifteen minutes later than a per-tick retry would have.
- An outage longer than the three attempts, such as a registry down for an
  hour, now ends in a hold. It needs a retry or a push, like an environment
  failure after start already does under ADR-0062.
- With the cause in the error text (ADR-0063), the three failures normally
  carry identical text and collapse into one history record (ADR-0056).

## Alternatives considered

- **Hold at once when a `RUN` step fails.** The failure is reliably
  *recognisable*: BuildKit's verdict `process "…" did not complete
  successfully: exit code: N` is specific to a failed `RUN`. But it is not
  reliably *deterministic*. `RUN` steps fetch from the network all the time
  (`apt-get`, `pip`, `curl`), and an unreachable mirror makes apt exit 100
  exactly as an unsatisfiable pin does. Holding at once would strand a
  transient mirror outage until someone clicks. Three attempts are cheap, and
  the hold follows within fifteen minutes anyway.
- **Backoff without a hold.** This is the loop ADR-0062 rejected, only slower:
  a broken Dockerfile would still be rebuilt every few hours, forever.
- **A separate record next to `held`.** That would mean a second state map,
  gauge, roster field and release logic for what is a hold with a deadline. The
  release rules are identical, so they live in one place.
- **Counting runs instead of time.** Webhook-triggered runs would use up the
  backoff in seconds, and the wait would depend on the reconcile interval.
