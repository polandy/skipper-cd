# ADR-0024: Upcoming-deploys look-ahead

Status: accepted
Date: 2026-07-14

## Context

The header [deploy indicator](../../internal/ui/UI_SPEC.md) shows only the
*active* deploy — the ship glyph plus the currently-deploying stack name. During
a multi-stack run there is no signal for what deploys **next**, even though the
run's order is fixed once it starts.

The order alone is not enough to show. `DeployAllStacks` iterates the configured
stacks and decides *per stack, inside the loop* whether each has changed (hashes
vs. persisted state) — so at any moment the stacks after the current one are not
yet evaluated. Naively listing the remaining configured stacks would show ones
that will actually be skipped (unchanged) or deferred (autosync paused).

This is distinct from the autosync **pending queue**, which tracks deploys
*deferred across runs* because sync is paused. The look-ahead is the remaining
work of the **active run**.

## Decision

Compute the run's deploy set upfront and stream it to the UI as a new SSE state
snapshot; render it as a look-ahead trail and a read-only panel.

### An upfront planning pass

After the git sync and before the deploy loop, `computeRunPlan` hashes every
stack once (a read-only pass) and returns, in deploy order, those that will
actually deploy: changed **and** not autosync-paused. `_nixos` is excluded — the
rebuild has no per-stack deploying state in the UI and may restart skipper.

The planning pass runs **only when a run-plan sink is installed** (i.e. the UI is
on), so a headless deploy does no extra hashing. The deploy loop still
re-evaluates each stack independently and remains the single source of truth for
what is deployed; the plan is a display-only look-ahead. The two agree because
they read identical inputs under the deploy lock, after the sync — nothing
changes between them.

Hashing twice (once to plan, once to deploy) is accepted: the files are small,
runs are webhook-driven (infrequent), and a separate read-only planning pass is
clearer than threading cached hashes through the deploy path.

### The `upcoming` SSE snapshot

`RunPlan{ Upcoming []string }` is published over the existing state-event stream
(alongside `autosync`/`queue`): the stacks that come **after** the one now
deploying, republished as each stack starts and emptied when the run ends. The
latest snapshot is also served as SSE initial state so a client connecting
mid-run sees the look-ahead immediately. The active stack itself stays sourced
from the existing `deploying` events — the snapshot only adds the look-ahead, so
there is no second, potentially-disagreeing source for "who is deploying".

### UI surface

The [deploy indicator](../../internal/ui/UI_SPEC.md) gains a dimmed trail
(`→ a · b · +N`, capped) after the active name, collapsing to a compact `+N`
count chip on mobile. Clicking the indicator opens a read-only **run panel**
styled like the autosync drawer, listing the active deploy (ship badge) and the
upcoming stacks in order. Read-only fits skipper-cd's viz-only scope — the panel
shows the run, it does not trigger or reorder it.

## Consequences

- The header answers "what's next?" during a run without the operator watching
  the table scroll.
- The extra work is one hashing pass per run, gated on the UI being enabled; the
  deploy path is otherwise untouched, so the invariants around change detection
  and ordering are unaffected.
- The plan is a look-ahead, not a contract: if a planned stack somehow fails to
  deploy (e.g. a hash error at deploy time that did not occur at plan time), the
  trail briefly lists a stack that then does not emit a `deploying` event. This
  is transient and self-correcting on the next snapshot, and cannot happen under
  normal operation (identical inputs under the lock).
