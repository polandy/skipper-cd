# ADR-0033: Durable per-stack deploy audit log

Status: accepted
Date: 2026-07-18

## Context

Every deploy already emits a `DeployEvent` carrying the four things an audit
trail wants — **when** (`Timestamp`), **what** (`ChangedFiles`, `Commits`,
`Diffs`), **how long** (`DurationMs`), and **result** (`Status`). What is
missing is *retention and per-stack access*, not new data.

Today those events land in one structure — `events.History`:

- It is a **single global ring buffer capped at 100 events** across *all*
  stacks. On a host with ~27 stacks (the nuc), a handful of active stacks evict
  everyone else's records within a day. The cap exists to bound the live feed,
  not to preserve a history.
- It has **no per-stack query**. The Deploys view pulls the whole 100-event
  window over SSE and filters client-side; "show me every deploy of `immich`"
  is impossible the moment those records roll off the end.
- It **conflates three jobs**: real-time SSE catch-up (`EventsAfterID`),
  diff-on-demand (`EventByID` → `/api/events/{id}/diffs`), and the *de facto*
  history. Diffs (potentially large) sit in the same file that is rewritten
  wholesale on every `Add`.

The result: skipper can tell you what happened in the last ~100 events, but not
"what is the deploy history of this one stack" — the audit question. This stays
within skipper's scope ([[project-scope-visualization-not-trigger]]): it records
and displays deploys git already drove; it adds no trigger and no action.

## Decision

Introduce a **separate, durable, append-only audit log** — a new
`internal/audit` package — recording **terminal deploy outcomes per stack** as
compact metadata records. The live `events.History` is left exactly as it is
(recent cross-stack feed + diffs); the two stores get distinct, single
responsibilities.

### What is recorded

Only **terminal outcomes** — the states that mean "something happened to this
stack and here is how it ended":

`success`, `failed`, `rolled_back`, `rolled_back_unhealthy`, `healed`,
`heal_exhausted`.

Deliberately **excluded**:

- `deploying` — an in-progress marker, not an outcome; its terminal event is
  recorded instead.
- `skipped` — nothing changed; auditing "checked, no-op" is pure noise.
- `queued` / `blocked` — deferrals, not outcomes. When the deferred change
  finally deploys it produces a real terminal record; the deferral itself is a
  live-feed concern.

### Record shape (metadata only, no diffs)

```
AuditRecord {
  Stack        string
  Timestamp    time.Time
  Status       events.Status   // one of the terminal set above
  DurationMs   int64
  CommitSHA    string          // newest deployed commit (short form in UI)
  ChangedFiles int             // count, not the list — the audit answers "how much", the diff answers "what"
  Error        string          // present for failed / rolled_back*
}
```

**No diffs in the audit log.** Diffs stay in the live ring and are fetched on
demand while that event is still present. An audit record references its commit
SHA, so the change is reconstructable from git if ever needed. The audit answers
*when / result / how long / which commit / how many files*; it is not a code
archive.

### Storage: append-only NDJSON

One file `deploy-audit.jsonl` in the state dir, one JSON record per line.

- **Append is O(1)** — a single line appended, no wholesale rewrite (the current
  YAML ring re-marshals all 100 events on every `Add`).
- **Crash-robust load**: a torn trailing line from a mid-append crash is skipped,
  not fatal — the rest of the log loads.
- Empty state dir disables persistence (in-memory only), matching `History`.

### Retention: per-stack cap

Keep the most recent **N records per stack** (proposed default **200**), not a
global cap — so one busy stack can never evict another's history, the exact flaw
being fixed. Over-cap records are dropped during a **compaction** that rewrites
the file (temp-file + rename, atomic), run at startup and when a stack first
exceeds cap + slack. 200/stack × ~30 stacks ≈ a few MB worst case — bounded and
trivial for a homelab.

*Alternatives considered:* unbounded (true audit) — rejected for a
long-running service that would grow without limit; age-based (e.g. 90 days) —
rejected as less predictable than a count for low- and high-frequency stacks
alike. A generous per-stack count keeps "recent history", which is what the
question actually wants.

### In-memory model & API

`audit.Log` owns the records (indexed by stack) and the file; it exposes
`Record(DeployEvent)` (filters to terminal, appends, persists) and query methods
returning copies — no raw slice or map leaks (encapsulation). Reads serve from
memory, never touching disk per request.

- `GET /api/audit?stack=<name>&limit=<n>` — one stack's records, newest first.
- `GET /api/audit` — recent records across all stacks (for a global timeline).

### UI

A read-only **per-stack history panel** reachable from the Deploys view: the
full timeline for one stack — each past deploy as time · result pill · duration ·
commit short-SHA · changed-file count. It complements, and is visually distinct
from, the live cross-stack feed. Details in `internal/ui/UI_SPEC.md` before the
UI change.

### Wiring

`main.go` already runs one subscriber that calls `history.Add(e)`. Add
`auditLog.Record(e)` alongside it — the terminal-status filter lives inside
`audit`, so no new event plumbing and no change to producers.

## Consequences

- A durable, per-stack deploy trail that survives ring eviction **and** restarts,
  answering the audit question the 100-event global ring cannot.
- Clean split of responsibility: `History` = recent, cross-stack, diff-bearing
  live feed; `audit.Log` = long, per-stack, metadata outcome trail. Neither
  grows the other's concerns.
- Append-only NDJSON removes the wholesale-rewrite cost the YAML ring pays on
  every event.
- Old deploys show metadata, not diffs. Acceptable by design; a later
  git-fetch-by-SHA could restore diffs for archived records if ever wanted.
- One new bounded file on disk (a few MB worst case).

## Decisions confirmed (2026-07-18)

1. **Separate `audit` package** (not an extension of `History`) — the two have
   different lifetimes and shapes; `History`'s live-feed/diff duties stay free of
   retention logic.
2. **Retention = per-stack count, default 200.**
3. **Terminal outcomes only** (`success`, `failed`, `rolled_back`,
   `rolled_back_unhealthy`, `healed`, `heal_exhausted`).
4. **Metadata-only records** — no diffs in the audit log.
