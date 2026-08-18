# ADR-0055: A standing configuration error is reported once, not every run

Status: accepted
Date: 2026-08-18

## Context

Stack discovery (ADR-0034/ADR-0043) validates the stack set on **every** sync,
and a reconcile tick runs every few minutes whether or not anything was pushed.
Two kinds of failure come out of that validation:

- **entry-level** — an override naming a stack that has no directory, an
  unparseable compose file, a rollout referencing an unknown service, a missing
  `env_files` path. The affected stack is excluded, the rest deploy.
- **file-level** — a leftover in-repo `skipper.yaml`, an unreadable base
  directory. Nothing about the set can be trusted, so no stack deploys.

Both were reported the same way a failed deploy is: `emitDeployFailure` for the
stack (or the reserved `_config` key), which increments
`skipper_deploy_errors_total`, logs at error level, writes a `failed` record
into the history — and, because `failed` is a terminal status, sends a
notification.

That is right for a deploy: a deploy is an event, it happened once, it failed.
It is wrong for a configuration error, which is a **state**. Nothing happens on
the following tick; skipper reads the same broken configuration and reaches the
same conclusion. Reporting it again claims an event that did not occur.

Observed on a live host: one override left behind after a stack was renamed
produced 191 `failed` events and 191 push notifications in 16 hours — one every
five minutes, all identical, for a single unfixed line of YAML. The signal that
mattered was in the first one; the remaining 190 trained the operator to ignore
the channel. Meanwhile the actual outage signal — a *deploy* failing — shares
that channel and that counter.

## Decision

**An entry-level or file-level configuration error is announced when it appears
and whenever its message changes, and stays quiet in between.** The deployer
remembers the last message reported per stack; a run that reads the identical
message again logs at debug level and emits nothing.

Only the *reporting* is deduplicated. The run itself is unchanged: the stack is
still excluded, its dependents are still blocked, and the gate still records the
outcome — a broken stack does not silently deploy.

Because "no event" cannot express "still broken", the standing state gets its
own instrument: the gauge **`skipper_stack_config_error{stack}`**, set to 1
while the error stands and deleted when it clears. That is what an alert on a
lingering broken configuration reads; `skipper_deploy_errors_total` goes back to
meaning what its name says — deploys that ran and failed.

Clearing is derived from the run, not tracked separately: every run reports the
full set of currently-broken stacks, so any remembered stack absent from that
set has been fixed (or has left the repo). Its memory and its gauge are dropped
and a `stack configuration error resolved` line is logged. A stack whose error
clears rejoins the normal path and emits its own deploy event, so no separate
resolution event is needed.

The memory is deliberately **in process, not persisted in `state.yaml`**. A
restart re-announces every standing error exactly once, which is the reminder an
operator wants when skipper comes back up, and it keeps a safety-critical file
free of a purely cosmetic field. A restart happens on a switch or an upgrade —
orders of magnitude rarer than the reconcile tick this ADR is about, so the
noise is removed either way.

The file-level error additionally carries its consequence in the message itself
(`stack discovery failed, no stacks deploy this run: …`) rather than only in a
neighbouring log line, so the one event that is emitted says what it means in
the UI and in the notification too.

## Consequences

- **The history stops re-stating a standing error.** A stack broken for a week
  shows one `failed` record at the time it broke, not two thousand. The Stacks
  view keeps showing that outcome as the stack's last one, which is accurate.
- **Its timestamp ages.** A stack still excluded reads "failed 6 hours ago",
  because that is when the error was last *new*. The gauge is the live view.
- **A changed message is a new report.** Fixing one problem and hitting another
  (missing directory → unparseable compose) is announced, since the operator
  needs to know the reason moved.
- **Monitoring must move to the gauge.** An alert built on
  `increase(skipper_deploy_errors_total[5m])` will no longer keep firing for a
  broken configuration — by design; `skipper_stack_config_error` is the
  replacement and is documented in `docs/metrics.md`.
- **Nothing to configure.** No key, no log-level knob. The behaviour is the
  correct one for both the noisy and the quiet case.

## Alternatives considered

- **A `log_level` / verbosity key.** Rejected for the same reason as in the
  idle-run cleanup: the problem is not that the line is loud, it is that the
  line is *wrong* — it announces a fresh failure where nothing happened. A knob
  would also have suppressed the first, genuine report along with the repeats,
  and it turns a fix into an operator decision.
- **Suppressing only the notification.** Rejected: it treats the symptom on one
  channel. The event stream, the error counter and the run summary carried the
  same false claim, and a UI history full of identical failures is its own kind
  of noise.
- **Persisting the dedup in `state.yaml`.** Rejected: it adds a field to the
  file that drives change detection for the sake of silence across restarts,
  and one re-announcement per restart is useful rather than unwanted.
- **A rate limit (report at most once an hour).** Rejected: it still invents
  events. The number of times a standing error is announced should be governed
  by whether it changed, not by a clock.
- **A dedicated `config_error` event status.** Rejected: it would need a UI
  surface, a notification formatter and a place in the roster's status model,
  to say something `failed` plus the exclusion already say correctly. The
  frequency was the defect, not the vocabulary.
