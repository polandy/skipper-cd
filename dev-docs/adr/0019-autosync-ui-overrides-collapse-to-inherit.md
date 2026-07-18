# ADR-0019: Autosync UI overrides collapse to inherit when they match the baseline

Status: accepted
Date: 2026-07-13

Extends [ADR-0016](0016-autosync-and-queue-via-leave-dirty.md).

## Context

ADR-0016 introduced two autosync scopes (global + per-stack) in two layers
(config-as-code + in-memory UI override) and the resolution rule

```
effective(stack) = stackOverride ?? stackConfig ?? (globalOverride ?? globalConfig ?? true)
```

A per-stack value wins over global in both directions. That resolution is
correct, but the *interaction* between the global switch and the per-stack
switches was surprising, because a per-stack UI override, once set, was a
permanent pin until an explicit clear — and the UI never sent a clear:

- **Sticky redundant pins.** Global on, pause stack A, resume stack A. The
  resume posted `enabled: true`, which stored an explicit `true` override.
  Visually A looked like an inheriting stack again, but it no longer followed the
  global switch: a later global-off left A surprisingly syncing. The global
  switch appeared to "stop working" for every stack the user had ever touched.
- **No path back to inherit from the UI.** The two-position per-stack switch only
  ever flipped between explicit-`true` and explicit-`false`; `SetStack(name, nil)`
  (fall back to global/config) was reachable from the API but never sent by the
  UI, so overrides accumulated invisibly.

The felt bug was "the global master switch doesn't cascade." We want the global
switch to behave like a real master, and the per-stack switch to mean *this is an
exception to global*, not *pin this stack forever*.

## Decision

### A UI override is only ever an exception to its baseline

Define a stack's **baseline** as what it would resolve to *without* its UI
override — its config value if set, otherwise the effective global:

```
baseline(stack) = stackConfig[stack] ?? globalEffective()
```

A per-stack **UI override is held only while it differs from the baseline.** The
moment it would equal the baseline, it does not exist. This is enforced at the
two points that can make them coincide:

- **On `SetStack(name, v)`** — if `v == nil` **or** `v == baseline(name)`, the
  override is cleared (fall back to inherit); otherwise it is stored. So toggling
  a stack to the value it would inherit anyway never creates a pin, and toggling
  it back to the baseline is the UI's natural "return to inherit" gesture — no
  separate reset control is needed.
- **On `SetGlobal(...)`** — after the global override changes, every per-stack UI
  override whose value now equals its baseline collapses to inherit. A genuine
  exception (still differing from the new baseline) survives.

Config-as-code values never collapse: a `stacks[].autosync` in `skipper.yml` is a
deliberately durable exception and pins the baseline, so a global toggle does not
touch config-pinned stacks at all. Only in-memory UI overrides are "soft".

### Consequence: the global switch is a true master

With redundant overrides gone, toggling global moves every inheriting stack and
leaves only genuine exceptions standing. In particular, a per-stack pause set
through the UI does **not** survive a global off→on cycle: turning global off
makes the paused stack's baseline `off`, its override (`off`) equals the baseline
and collapses, so turning global back on resumes it along with everything else.
This is the chosen semantic — a UI pause is an exception relative to the current
global baseline, not an independent latch. Durable, latch-style pauses belong in
`skipper.yml` (config-pinned), exactly as ADR-0016 intended.

### Scope: stack overrides only

The collapse rule applies to per-stack UI overrides. A **global** UI override
equal to its config value is left as-is (deliberately out of scope): nothing
renders `global_overridden`, the global switch always reflects and always toggles,
and a restart drops it regardless — so there is no sticky-pin problem to solve at
the global scope. Keeping the rule to one scope keeps the mental model small.

## Consequences

- No change to the resolution formula, the leave-dirty queue, the pending
  registry, or the deploy lock (ADR-0016 and invariant 7 stand). The change is
  local to how `Controller` stores overrides.
- The UI needs no logic change: it already posts `enabled = !current`; the server
  reinterprets "set to the baseline value" as "clear the override". The
  `overridden`/`config`/`effective` snapshot fields already let the drawer render
  the resulting inherit vs. forced state honestly.
- The per-stack switch gains an intuitive "back to global" gesture (toggle to the
  inherited value) without a third state or a reset affordance.
- A UI pause cannot outlive a global off→on cycle; operators who need a pause to
  persist declare it in `skipper.yml` (already the only way to persist a pause at
  all, per ADR-0016).
- Full behaviour lives in [`dev-docs/autosync-spec.md`](../autosync-spec.md#override-collapse);
  the UI surface and its E2E coverage (UC10–UC12) in
  [`internal/ui/UI_SPEC.md`](https://github.com/polandy/skipper-cd/blob/main/internal/ui/UI_SPEC.md) and
  [`dev-docs/e2e-tests.md`](../e2e-tests.md).
