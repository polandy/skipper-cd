# Feature Spec: Sync Windows

Status: proposal (not accepted)
Date: 2026-07-18

## Goal

Time-based deploy gating — ArgoCD's sync windows, homelab-sized: "don't
deploy media stacks while we're watching a movie", "no deploys overnight
so a bad Renovate bump doesn't take the house down at 3 AM". With
autosync-always-on plus automated image updates, a merge deploys
*immediately* today; windows add "…but only at acceptable times".

Non-goals: cron-expression schedules, one-off calendar windows, allow-list
semantics (deny-only keeps the model simple), gating of self-heal or
health-watch (they *restore* the deployed version — that must never wait),
manual override buttons (the existing non-persistent UI autosync toggle
already covers "let it through now": turning autosync off and on re-syncs).

## User model

```yaml
deny_windows:              # global, optional
  - days: [mon, tue, wed, thu, fri, sat, sun]   # optional, default all
    from: "22:00"          # local time of the skipper host
    to:   "06:00"          # may wrap past midnight
stacks:
  - name: media
    deny_windows:          # optional per-stack windows, ADDED to global
      - days: [fri, sat]
        from: "19:00"
        to:   "23:59"
```

- Deny-only: outside every window, deploys run as today. Per-stack windows
  extend (union), never replace, the global ones.
- Times are host-local (homelab: the wall clock people live by), minute
  resolution, `to` exclusive; `from > to` wraps past midnight.

## Semantics: reuse the queue, add nothing new

A stack whose deploy is due inside a deny window behaves **exactly like an
autosync-off stack** (ADR-0016): the sync leaves it dirty, registers it in
the pending registry, and emits the existing `queued` event — with a new
`reason: "window"` field on the event so the UI can label it. No second
queue, no new gate mechanism; the window check is one more input to the
existing per-stack deploy gate.

- When the window closes (time passes `to`), the next sync deploys the
  queued work. With the reconcile loop on (default, 300 s), that happens
  within one interval of the window edge — **no new timer**. With
  reconcile disabled, queued work waits for the next webhook; documented,
  and reconcile-on is the recommended setup anyway (ADR-0028).
- The window is evaluated per stack at deploy time, under the deploy
  mutex — a sync that starts at 21:59 and reaches the stack at 22:01
  queues it (edge case accepted; the alternative, evaluating at sync
  start, is no less arbitrary).
- Interactions:
  - `depends_on`: a windowed (queued) dependency defers its dependents,
    same as any not-yet-deployed dependency this run.
  - Self-heal / health-watch / rollback: **unaffected** — windows gate
    *new versions*, never restoration of the current one.
  - `_nixos`: **not** gated by stack windows. A deferred nixos-rebuild
    interacts badly with the pre-saved-hashes design (ADR-0005/0015) and
    with skipper self-restarts; if nixos gating is ever wanted it needs
    its own ADR. Documented limitation.
  - Upcoming-deploys indicator (ADR-0024): a windowed stack shows in the
    run plan as queued — the header indicator is the natural "it's
    waiting" surface.

## UI

- Queued-because-of-window rows reuse the existing `queued` chip with a
  clock glyph + tooltip naming the window ("queued · window until 06:00").
- No window-editing UI — config-file only, like everything else.
- `internal/ui/UI_SPEC.md` addendum before implementation.

## Package layout

- New `internal/windows` package: types, parsing/validation
  (`"HH:MM"`, day names, wrap), and `Denies(stack string, at time.Time)
  bool` — pure logic, clock injected for tests.
- `internal/config`: `deny_windows` global + per-stack; validation errors
  name the offending window.
- `internal/deploy`: one call in the per-stack gate, alongside the
  autosync check; `queued` event gains the optional `reason` field
  (backward-compatible: omitted = autosync as today).

## Testing

- `internal/windows`: table tests — inside/outside, midnight wrap, day
  filter, `to` exclusivity, DST transition days (the two ambiguous hours;
  assert defined behavior: local-time comparison, accept the fuzz).
- `internal/deploy`: stack inside window → no compose argv, `queued` event
  with `reason: window`, hashes untouched; window open on next sync →
  deploys; dependent of a windowed stack defers.
- e2e: none needed beyond the existing queued-chip mask unless the UI
  variant lands.

## Open questions

1. Per-stack `deny_windows` at all, or global-only for v1? The media
   example wants per-stack; cost is small since it's a union. Proposed:
   both, it's the same code path.
2. Event `reason` field vs. a distinct `queued_window` status? Proposed:
   field — the queue/pending machinery treats both identically, and
   status strings are load-bearing across UI + notifications + audit.
