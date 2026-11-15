# ADR-0061: Each stack rolls back to the commit it last ran

Date: 2026-09-29

## Status

Accepted

## Context

A rollback restores a stack's compose file from `last_deployed_commit`, and a
deploy's diff and commit list are computed against it. That commit is global:
`finishRun` advances it to `HEAD` at the end of every run in which nothing is
queued. A stack whose deploy *failed* in that run does not hold it back.

So the first attempt of a broken change rolls back correctly, to the version
before the push. The run then ends, `last_deployed_commit` moves to the broken
commit, and the change is still pending. The next reconcile tick retries it,
the retry fails the same way, and its rollback restores the compose file from
the new base: the broken version itself. From then on every retry reports
`rolled_back_unhealthy` ("restored version did not come up healthy"), the stack
stays on the broken version instead of the one that worked, and the retry's diff
is empty because it is computed `HEAD..HEAD`.

This was seen in production: a `signal-cli-rest-api` image bump rolled back
once, then looped every five minutes for five hours with the container on the
broken image, and the loop's events evicted the stack's whole history.

A dependent blocked by the failure already pins the base (it sits in the
pending registry), but the failed stack itself does not. Pinning the global
base behind every failing stack would stop the base for *all* stacks while one
stays broken, so their diffs would grow to include changes long deployed.

## Decision

Each stack keeps its own base commit, `stack_commits.<name>` in `state.yaml`:
the `HEAD` of the last run that deployed it or found it unchanged. Its
rollback restores from that commit, and its deploy, queued and blocked events
compute their diffs, commits and service attribution against it. A stack whose
deploy fails is not settled, so its base stays where it last worked, however
many runs retry it.

A stack with no entry yet falls back to `last_deployed_commit`, which is what a
state written before this change holds. To close the upgrade window, `finishRun`
first pins every recorded stack without an entry to the current global base,
then moves the settled stacks to `HEAD`, and only then advances the global base.
A stack failing in its first run after the upgrade therefore keeps the pre-run
commit too.

`last_deployed_commit` stays, with a narrower job: the diff base of the run
phases that are not a stack (`_nixos`) and the fallback above. It keeps the
rule that it does not advance while a change is queued. A stack's own base
advances regardless, since the stack itself did deploy at `HEAD`.

## Consequences

- A retried broken change rolls back to the version that last worked, on every
  retry, and its diff shows what is actually pending.
- A removed stack's entry is dropped with its hashes and images.
- Reserved run-phase keys (`_nixos`, `_config`, `_project_dir`) never get an
  entry.
- This makes retries correct, not rarer. Retrying a change that rolled back
  over and over is a separate question.
