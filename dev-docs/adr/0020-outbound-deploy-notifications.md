# ADR-0020: Outbound deploy notifications via a generic HTTP sink

Status: accepted
Date: 2026-07-13

## Context

skipper-cd already produces a `DeployEvent` for every terminal outcome —
`success`, `failed`, `rolled_back` (plus `deploying`, `skipped`, `queued`) — and
fans it out to SSE subscribers and the persisted history (ADR-0016, ADR-0013).
That is entirely *pull*: someone has to be looking at the UI, the `/api/events`
stream, or `state.yaml` to learn a deploy failed. For an unattended homelab CD
tool the common need is the opposite — **push me a message when a deploy fails or
rolls back**, without keeping a browser tab open.

There is no outbound notification of any kind today. We want to add one while
keeping the project's line: lightweight, no per-provider SDK sprawl, no new heavy
dependency, deploys never blocked or wedged by a slow/broken notifier.

Constraints and forces:

- The current event sink is wired **only when `cfg.UIEnabled`**
  (`cmd/skipper/main.go`, `deployer.SetEventSink`). Notifications must work with
  the UI **off** — they are an independent concern.
- Homelab targets are heterogeneous: Slack, Discord, ntfy, Gotify, Matrix
  (via a bridge), a personal endpoint. Their request bodies differ
  (`{"text":…}` vs `{"content":…}` vs a plain-text body with headers), so a
  single fixed JSON POST does not "just work" everywhere.
- Delivery must not sit on the deploy mutex or the shutdown path
  (invariant 7, ADR-0007): a hung webhook must not delay the next deploy or a
  graceful stop.
- Testing convention: no real network. Assert the exact outbound request through
  an injected fake, exactly as the `Runner` fakes assert docker/git argv
  (ADR-0003).

## Decision

### One outbound HTTP sink, provider shape chosen by a `Formatter`

Add an `internal/notify` package with a `Notifier` that subscribes to
`DeployEvent`s and, for each event matching a target's filter, builds and sends
**one HTTP request**. The provider-specific shaping is a small consumer-side
interface:

```go
// Formatter turns a DeployEvent into the concrete HTTP request a given
// provider expects (method, headers, body).
type Formatter interface {
    Format(events.DeployEvent) (*http.Request, error)
}
```

v1 ships two formatters, selected by a `format:` field per target:

- `signal` — `POST {url}/v2/send` with
  `{"message": "<rendered message>", "number": "<sender>", "recipients": [...]}`,
  the `bbernhard/signal-cli-rest-api` shape. `url` is the service base
  (e.g. `http://localhost:8020`); the formatter appends `/v2/send`. This is the
  homelab driver for the ADR — skipper already deploys a `signal-api` stack, so
  it can notify through it.
- `generic` — `POST {url}` with the full `DeployEvent` JSON body and any
  configured static `headers` (covers ntfy/Gotify/custom endpoints; the token
  rides in the URL or a header).

`slack` and `discord` formatters (`{"text": …}` / `{"content": …}`) were
prototyped but **cut before merge** because they could not be exercised on the
maintainer's homelab; shipping untested provider shapes contradicts the
test-first principle. They are the obvious next `Formatter`s to add once there is
a way to verify them.

The interface is the extension point: a new provider is a new `Formatter`, no
change to the send/retry/dispatch machinery. A raw Go-template escape hatch is
deliberately **out of scope for v1** (see Alternatives) and can be added later as
just another formatter.

`signal` needs sender/recipient identity that a single `url` cannot carry, so the
target struct has two **`signal`-only** fields, `number` (sender) and
`recipients` (non-empty list); they are ignored by the other formats and required
only when `format: signal`.

### An additional sink, independent of the UI

The notifier is composed into the deployer's event sink alongside — and
independently of — the UI history/broadcaster. Concretely, `main.go` builds the
sink from whatever consumers are configured:

- UI enabled → history + broadcaster (unchanged).
- `notifications:` present → the notifier.
- both → both, fanned out in one sink closure.

So notifications require neither `ui_enabled` nor a running browser. The sink
stays a single `func(events.DeployEvent)`; it just calls more than one consumer.

### Terminal events only, filterable, default = all terminal

The notifier only ever considers terminal statuses: `success`, `failed`,
`rolled_back`. `deploying`, `skipped` and `queued` are never delivered (noise).
Each target declares which of the three it wants via `on:`; the **default is all
three (`[failed, success, rolled_back]`)** — a plain target reports every
completed deploy, and a quieter target opts down to `[failed, rolled_back]`.

### Fire-and-forget, bounded, never on the deploy path

`Notify` enqueues onto a small buffered channel and returns immediately; a
background worker does the HTTP send with a per-request timeout (default 10s,
separate from `command_timeout_seconds`). This mirrors the webhook handler's
"respond 202, work in a goroutine" shape and the transient-unit ethos of
ADR-0014: the deploy goroutine is never blocked by a slow notifier.

- Failures (non-2xx, timeout, connection error) are **logged and dropped** — at
  most one bounded retry, no persistent queue. A CD notification is worthless
  once stale; we do not build a durable outbox.
- If the buffer is full (notifier wedged), new events are dropped with a warning
  rather than blocking the sink — same slow-consumer stance as the log ring
  (ADR-0013).
- On shutdown the worker drains best-effort within a short deadline, then exits;
  the wait for it is abandoned like the rebuild wait (ADR-0014), never blocking
  the stop.

### Config shape

A top-level `notifications:` list, each entry a target:

```yaml
notifications:
  # Signal via the signal-api stack skipper itself deploys.
  - format: signal
    url: http://localhost:8020        # signal-api base; /v2/send is appended
    number: "+491234567890"           # sender (signal-only, required)
    recipients: ["+491234567890"]     # recipients (signal-only, non-empty)
    # on:  defaults to [failed, success, rolled_back]

  # A second, independent target (raw DeployEvent JSON) for failures only.
  - format: generic
    url: https://ntfy.example/skipper
    headers: { Authorization: "Bearer <token>" }
    on: [failed, rolled_back]         # subset of failed|success|rolled_back
```

Absent/empty section → notifications fully disabled, zero overhead. Validation
lives in `internal/config` next to the other target validation: `url` required
and well-formed, `format` in the known set (default `generic`), `on` a subset of
the terminal statuses, and — when `format: signal` — `number` set and
`recipients` non-empty (both rejected on any other format). Secrets in
`url`/`headers` follow the same handling as `webhook_secret` today; `${ENV}`
expansion is noted as a follow-up, not part of this ADR.

**Reachability note (not ADR-level, but the reason `url` is a base):** skipper
runs as a host systemd service while `signal-api` sits on a docker network
exposed at host port `8020` (`8020:8080`). The host-reachable target is therefore
`http://localhost:8020`, which hits the container directly and bypasses the
Traefik/authelia route on `signal-api.${DOMAIN}` — exactly the unauthenticated
direct path we want for an internal notifier.

### Metrics

`internal/metrics` gains `skipper_notifications_sent_total{format,outcome}`
(`outcome` = `ok`|`error`) and `skipper_notifications_dropped_total` (buffer
overflow), consistent with the existing counter style.

## Consequences

- Deploy failures reach a human with the UI off and no tab open — the main goal.
- New surface area: one package, a config section, two formatters, two
  counters. The `Formatter` interface keeps provider growth additive.
- The event sink is no longer UI-only; `main.go` composes it from the configured
  consumers. Small refactor, no change to `DeployEvent` or the deployer API.
- No durable delivery guarantee by design: a message can be lost if the target
  is down. Acceptable for notifications; anything needing at-least-once should
  read the persisted history or scrape metrics.
- New outbound-network behavior in a tool that was previously inbound-only —
  documented as such; a wrong/hung URL can only drop messages, never stall a
  deploy or the shutdown.
- **Self-notify dependency loop with `signal`.** The `signal-api` stack is
  itself deployed by skipper. If Signal is the *only* target and a `signal-api`
  deploy fails (or the container is down), the failure notification cannot be
  delivered — a chicken-and-egg gap. Not a design blocker: the persisted event
  history and metrics still record it. The mitigation is operational —
  configure a **second, independent** target (e.g. ntfy via `generic`) so at
  least one delivery path never depends on the stack being notified about.

## Alternatives considered

- **Per-provider clients (Slack SDK, Discord SDK, …).** Rejected: dependency
  sprawl against the lightweight/no-deps line. `net/http` + a `Formatter` covers
  the same providers.
- **Single fixed JSON POST, no formatter.** Rejected: does not natively satisfy
  Slack/Discord/ntfy body shapes; users would need a relay. The `generic`
  formatter still offers exactly this for endpoints that want the raw event.
- **Go `text/template` message bodies in v1.** Deferred: real power, but real
  footguns (escaping, JSON validity) and more than the 80% case needs. It slots
  in later as another `Formatter` without touching dispatch.
- **Durable outbox / retry queue with persistence.** Rejected: stale
  notifications have no value; the persisted event history already covers audit.
- **Native email (SMTP) delivery.** Rejected deliberately. Email is a different
  transport (SMTP/STARTTLS, auth, MX) that does not fit the HTTP `Doer` +
  `Formatter` model — supporting it natively would mean `net/smtp` plus TLS/auth
  handling, exactly the heavyweight surface the lightweight line avoids, for a
  channel that is slower to notice than push. Email is still reachable **today**
  by pointing a `generic` target at an email-capable relay (ntfy, Apprise,
  Shoutrrr), which also keeps SMTP credentials out of skipper. If native email
  were ever justified, the right shape is a transport-level `Sink` interface
  above `Formatter` in a follow-up ADR — not bolted onto this one.
- **Notify from inside `internal/deploy`.** Rejected: keeps `deploy` free of
  HTTP/provider knowledge (same separation nixos/metrics/events already enjoy);
  the notifier is a sink consumer wired in `main`, not a deploy dependency.

## Testing

`notify` tests inject a fake HTTP `Doer` (consumer-side interface, like
`Runner`) and assert the exact method, URL, headers and body each formatter
produces — including the `signal` `/v2/send` body with `number`/`recipients` —
plus: filtering by `on:`, non-terminal statuses never delivered, non-2xx →
error counter + no crash, buffer-full → dropped counter, shutdown drains without
blocking. `config` tests cover validation of the new section, including the
`signal`-only `number`/`recipients` rules. No real network, matching the repo's
fake-based convention.

## Amendment (2026-08-05): the terminal set has grown; it has one authority

This ADR enumerates the terminal set as three statuses (`success`, `failed`,
`rolled_back`) and calls the default "all three". Two later decisions added to
it — `rolled_back_unhealthy` (ADR-0022) and `heal_exhausted` (ADR-0029) — so the
set is now five, and the default is all five.

The second addition was only half-wired, and the shape of the gap is the point.
`heal_exhausted` was added to the config vocabulary (`NotifyOnHealExhausted`, in
the default `on` set, accepted by validation) and given its own formatter branch,
but the notifier's own `isTerminal` guard — the filter in `Notify` that drops
non-terminal statuses before they are queued — was never extended. A target
could subscribe to a status that could not be delivered. ADR-0029's high-signal
alarm ("a stack is down and I could not fix it") was therefore silently dropped
for its entire lifetime, and nothing failed: the config was valid, no error was
logged, the message simply never arrived.

The vocabulary in `internal/config` is the authority. `isTerminal` mirrors it and
must never be the narrower of the two; a status that is subscribable but not
deliverable is a dead subscription, and dead subscriptions do not announce
themselves. This is enforced by a test that walks every `NotifyOn*` constant and
asserts it is deliverable, rather than by a per-status test that has to be
remembered when the set grows again.

## Amendment (2026-08-05): one digest per run for successes; alarms stay individual

This ADR's delivery contract is one terminal event, one message. That is right
per *stack* and wrong per *push*: a single commit touching many compose files
deploys many stacks in one run, and each sends its own message. The run of
2026-08-05 deployed 16 stacks in 2m54s and sent 16 messages that all reported the
same thing — a change applied, verified healthy. The information was real; its
granularity was not. The operator's unit of attention is the run, because the run
is the unit of *cause*.

**Decision: a run's `success` events are collected into one digest, flushed at
the end of the run. Every other terminal status is delivered immediately and
individually, exactly as before.** An alarm is the message meant to interrupt
someone, and it is worth neither a delay nor the risk of being skimmed past
inside a long list of successes. So the digest becomes the deliberately boring
channel, and anything that is not fine leaves it and arrives alone — including
when both happen in one run, which then sends two messages by design.

The flush trigger is the **run boundary** (`PostRunHook`), not a wall-clock
interval. A timed window was considered and rejected: it has no relationship to
the cause (two pushes merge, one run splits), it adds latency that buys nothing
once co-delivery with alarms is rejected, and it cannot be tested without an
injected clock — the repo forbids tests that wait and hope. The signal travels
through the notifier's own queue as a sentinel rather than as a direct method
call, so it cannot overtake events still buffered ahead of it.

Configured per target as `digest: per_run | off`, defaulting to `per_run`; `off`
restores the behaviour this ADR originally specified. Per target rather than
globally, because a phone and a log sink want opposite things. The full
behaviour — message shape, degenerate runs, run-abort and shutdown handling — is
specified in [`../notification-digest-spec.md`](../notification-digest-spec.md).

Consequence to accept: the default changes delivery for every existing
deployment that does not set the key. Deliberate — the previous default is the
one the 16-message run showed to be wrong — and the `off` value is the exact
escape hatch back.
