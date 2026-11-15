# ADR-0056: A repeated outcome collapses into the record it repeats

## Status

Accepted

## Context

A stale `stacks:` override for one stack made every five-minute reconcile fail
the same way for 16½ hours. Measured on the live host afterwards:

- **199 of that stack's 200 audit records** were the identical failure
  (`no stack directory … with a docker-compose.yml`). Exactly one real deploy
  survived — the per-stack cap that exists so a busy stack cannot evict a quiet
  one's history did not stop a stack from evicting *its own*.
- **91 of the 100 slots** in the global event ring held the same failure. Every
  other stack's deploys over fourteen hours were down to nine rows.

ADR-0055 had already fixed the *cause* for this class: a standing config error
is reported once, not on every reconcile. That is the right fix for config
errors and does nothing for the general shape. A retry storm of
`rolled_back · unhealthy` — already seen once on the same host — floods
identically, and so will whatever loops next.

The UI already folds runs of identical outcomes when it renders them (the
deploy-history panel, ADR-0033). Folding is a display treatment: by the time it
runs, the records that got evicted are gone. The flood is not primarily noise,
it is **data loss by eviction**, and no renderer can undo it.

## Decision

A terminal outcome that merely repeats a stack's previous one **collapses into
it** instead of taking a slot of its own, in both bounded stores — the global
event ring (`internal/events.History`) and the per-stack audit log
(`internal/audit.Log`).

**What counts as a repeat.** Same stack, same status, same error text, and no
other outcome for that stack in between. Only the statuses an unchanged, still
broken stack re-produces on every tick are eligible: `failed`, `rolled_back`,
`rolled_back_unhealthy`, `heal_exhausted`. Progress statuses are deliberately
excluded — two successes in a row are two deploys, not one deploy seen twice —
and `healed` with them: a self-heal *did* something each time it ran.

**What the collapsed record carries.** `repeat_count` (how many occurrences it
stands for), `first_seen` (the oldest), and `timestamp` (the newest). Events
additionally carry `supersedes_id`, naming the event the new one replaced, so a
connected UI can drop that row rather than draw both. The count and the span are
what 199 rows actually told the reader; the rows themselves told them nothing
the first one did not.

**On load, existing runs fold too.** Both stores replay their persisted contents
through the same rule on startup, so a flood written before this existed stops
crowding the store after one restart instead of for another 100 events.

## Consequences

- A stack's own deploy history survives a failure loop of any length: one
  standing failure costs one slot, not the whole ring.
- **The stores stop being strictly append-only.** A record can now change after
  it was written — the audit log's backing file is rewritten (it is already
  compacted in place, so the machinery existed) and an event is replaced in the
  ring. This is the real cost of the decision. The alternative that keeps
  append-only — capping how much of the ring one stack may hold — discards the
  data instead of compressing it and leaves the stack's own history just as
  gone, so it buys strictness with the thing we were trying to save.
- An event a client already holds can be superseded. `supersedes_id` is what
  keeps the UI consistent; a client that ignores it shows a stale duplicate
  rather than something wrong, and a reload corrects it.
- **The incident count is now a count of incidents.** The Stacks view's 24-hour
  badge previously reported a repeated failure once per reconcile tick — it
  described the reconcile interval more than the day. It now counts the
  incident once, which is what the badge always claimed to mean.
- **Notifications are out of scope here.** They are deduplicated on their own
  terms (ADR-0055 for config errors); a repeat still notifies exactly as before.
  Collapsing a history record and suppressing an alert are different decisions
  and are kept apart on purpose.
- The UI's display-time fold (ADR-0033) stays: it folds runs of *routine*
  outcomes with differing commits, which never collapse here. Its incident-fold
  branch now rarely has anything to do for fresh data, and remains correct for
  history written before this.
