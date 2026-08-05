# skipper-cd Notification Digest — Specification

Status: proposed — not implemented. The decision is recorded as the 2026-08-05
amendment to [ADR-0020](adr/0020-outbound-deploy-notifications.md), whose
original contract this replaces ("one terminal event, one message"); this
document is the behavioural detail behind it.

A single push that touches many stacks produces one notification per stack. The
run of 2026-08-05 deployed 16 stacks in 2m54s and sent 16 Signal messages that
all said the same thing — a change was applied and it came up healthy. The
information is real, its per-stack granularity is not: the operator's unit of
attention is the **run**, because that is the unit of *cause*. One push, one
message.

This spec keeps every alarm at per-stack granularity and batches only the
uneventful part.

---

## The rule

**A run's successes are collected into one digest. Everything that is not a
success is delivered immediately, on its own, exactly as today.**

| Status                  | Delivery                        |
|-------------------------|---------------------------------|
| `success`               | buffered, one digest per run    |
| `failed`                | **immediate, individual**       |
| `rolled_back`           | **immediate, individual**       |
| `rolled_back_unhealthy` | **immediate, individual**       |
| `heal_exhausted`        | **immediate, individual**       |

An alarm is the message that needs to interrupt someone, and it is never worth
delaying to keep a message count down. It also must not be read past: a failure
buried among fifteen successes in one long message is a failure that gets
skimmed. So the digest is deliberately the *boring* channel — "everything below
went fine" — and anything that is not fine leaves it and arrives alone.

A consequence to accept knowingly: a run in which one stack fails sends **two**
messages, the failure first (as soon as it happens) and the digest at the end.
That ordering is intended. The alarm is not held back to be co-delivered with a
summary, and the digest does not restate it.

`deploying`, `skipped`, `queued`, `blocked` and `healed` are not notifiable
today and stay that way — a digest of successes must not become a run report of
everything that did not happen.

## The unit is the run, not an interval

The flush trigger is the **end of a deploy run**, not a wall-clock timer. A
timed window was considered and rejected:

- It has no relationship to the cause. Two unrelated pushes inside one window
  merge into one message; one run that straddles a boundary splits into two.
  The 16:04 run took 2m54s — under a 5-minute raster it would have produced one
  or two messages depending on the phase it happened to start in.
- It adds latency to nothing useful. Successes gain a delay that buys no
  clarity, and the only reason to accept latency (co-delivering an alarm) is
  explicitly rejected above.
- It cannot be tested without racing it. A run boundary is a signal skipper
  already emits; a timer would need an injected clock to satisfy the repo's
  no-non-deterministic-timing rule, and the test would assert the clock rather
  than the behaviour.

There is therefore **no interval setting**, and adding one later would mean
re-opening the reasoning above, not just adding a key.

### The run boundary

`Deployer.PostRunHook` already runs once at the end of every run, after state is
saved, and is where `runTally` flushes the `run complete` log line. The digest
flushes at the same point.

**Ordering matters and is not free.** Events reach the notifier through a
buffered channel consumed by a background worker (ADR-0020: never on the deploy
path), while `PostRunHook` is called on the deploy goroutine. A direct
`Notifier.FlushRun()` method call would race the worker: a success still sitting
in the queue would be flushed into the *next* run's digest.

The flush signal therefore travels **through the same queue** as the events, as
a sentinel item, so the worker observes flush and events in emission order. The
queue's element type becomes a small sum (event or flush marker); nothing else
about the fan-out changes.

### Runs that end without `PostRunHook`

A failing `nixos-rebuild` and a stack-discovery config failure abort the run
before `PostRunHook` (Invariants 4 and 8) — the same hazard `runTally` documents
for its counts. Both abort *before* any stack deploys, so no success can be
buffered at that point and no digest can be misattributed to a later run. The
implementation must not rely on that being true forever: the buffer is keyed to
the run that filled it, and a run that ends by any path flushes or discards its
own buffer rather than leaving it for the next one.

On shutdown, the existing best-effort drain flushes a partial digest for the
in-flight run within the same deadline — a run's successes are never silently
dropped because a stop arrived mid-run.

## Degenerate runs

- **Zero successes** — no digest. A run of 30 skipped stacks sends nothing, as
  today.
- **Exactly one success** — the existing single-stack message is sent unchanged.
  A one-item digest would be strictly worse than the message it replaces: it
  loses the stack name from the headline and gains a count of one.

The digest shape is used from two successes upward.

## Message shape

Signal format, the 16:04 run as the worked example:

```
[nuc] ✅ 16 stacks deployed (2m54s)
  apc-monitor, authelia, changedetection, crowdsec, duckdns, gitea, homepage,
  immich, mealie, monitoring, nextcloud, nodered, paperless, signal-api,
  syncthing, vaultwarden
  ✓ 14 verified by a health gate
  • immich/server: v3.0.2 → v3.0.3
  • nextcloud/app: 30-apache@ab12… → 30-apache@cd34…
```

Rules:

- **Headline** — stack count and the run's wall-clock span, computed from the
  earliest buffered event's start (`Timestamp - DurationMs`) to the flush. Not
  the sum of per-stack durations, which would overstate a run's cost.
- **Names** — every stack, comma-separated, sorted. Knowing *which* stacks moved
  is the reason to read the message at all, so this list is not truncated.
- **Health gate** — one aggregate line, `✓ N verified by a health gate`, omitted
  when `N` is 0. It reports how many of the run's successes were gated, and
  deliberately does **not** read as "N of M passed": an ungated stack did not
  fail a gate, it has none. Per-stack `✓ health gate passed` lines
  (`renderHealthGate`) are dropped in digest mode — sixteen identical ticks
  carry no information the count does not.
- **Image changes** — only for stacks whose image reference actually moved,
  prefixed `<stack>/<service>` because the stack name is no longer in the
  headline. Same `versionTokens` reduction as today, so a version reads
  identically in a digest, a single message and the UI. Capped at
  `maxDigestImageLines` (10) with a trailing `• +N more` — this is the one
  unbounded part of the message and a run that rewrites every tag must not
  produce a wall of text.

The `generic` format posts one body per digest rather than per event:
`{"kind": "run_digest", "stacks": [...], "events": [...]}`, carrying the full
buffered `DeployEvent` array so a downstream consumer loses nothing. Rendering a
digest is a second method on the provider seam (`Formatter` gains
`FormatDigest`, or a sibling `DigestFormatter` interface) — a new provider stays
one type, as ADR-0020 intended.

## Configuration

Per target, alongside `on:`:

```yaml
notifications:
  - format: signal
    url: http://localhost:8020
    number: "+491234567890"
    recipients: ["+491234567890"]
    digest: per_run          # default; `off` restores one message per stack
```

- `digest: per_run` (**default**) — the behaviour above.
- `digest: off` — today's behaviour, one message per terminal event.

Per target, not global: a Signal target aimed at a phone wants the digest, while
a `generic` target feeding a dashboard or a log sink usually wants every event.
Both can be configured at once.

The default changes existing behaviour for every deployment that does not set
the key. That is deliberate — the current default is the one the 16-message run
demonstrated to be wrong — and it is a behaviour change worth a CHANGELOG entry
and a note in `docs/configuration.md`.

No interval key exists in any spelling. See [The unit is the run](#the-unit-is-the-run-not-an-interval).

### NixOS module

`services.skipper-cd.notifications.*.digest`, an enum of `per_run` / `off`,
default `per_run`, written straight through into `skipper.yml`.

## Metrics

Unchanged counters. `skipper_notifications_sent_total{format,outcome}` counts
one send per digest, which is the point — the metric measures delivered
messages, and the whole feature is about there being fewer of them.

The buffer is bounded like the queue it rides in. Overflow degrades the digest
(`• +N more`) rather than dropping a stack silently: a message that under-reports
without saying so is worse than a long one.

## Testing

- **`internal/notify`** — the flush is driven by the sentinel through the queue,
  never by waiting. Cases: two successes → one digest; one success → the
  unchanged single message; zero successes → no request; a failure inside a
  successful run → two requests, the failure first and not repeated in the
  digest; `heal_exhausted` never buffered; `digest: off` → one request per
  event; shutdown mid-run → partial digest delivered in the drain; a second run
  never inherits the first run's buffer.
- **Rendering** — table tests on the digest string: name list sorted and
  complete, gate count aggregated and omitted at 0, image lines capped with
  `+N more`, span computed from the earliest start rather than summed
  durations.
- **`internal/config`** — `digest` defaulting and enum validation, per target.

Ordering assertions read the fake `Doer`'s recorded request sequence; no test
sleeps, and none polls for delivery.
