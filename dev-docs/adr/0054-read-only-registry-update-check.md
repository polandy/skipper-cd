# ADR-0054: Read-only registry update check

Status: accepted
Date: 2026-07-31

## Context

[ADR-0030](0030-image-update-automation.md) settled who *applies* image updates:
delegate to Renovate (shape C), whose merge arrives as an ordinary push; a
Watchtower-style direct pull (shape B) is rejected outright, and an in-repo
watcher, if ever built, must be write-back-to-git (shape A). Nothing in this ADR
touches that decision — acting on a new upstream image remains a git commit,
written by a human or by Renovate.

What ADR-0030 left open is **visibility**. A host that does not run Renovate —
or a stack Renovate does not cover — has no way to learn, in the UI it already
watches, that upstream has moved on:

- A stack pinned to a **version tag** (`gitea:1.22.3`) shows exactly what it
  runs ([ADR-0053](0053-deploy-reports-running-image-versions.md)), but nothing
  says `1.22.6` exists. The operator finds out from release notes, or never.
- A stack on a **mutable tag** (`traefik:v3.1`, `:latest`) has the blind spot
  ADR-0030 names: the registry republished the tag, the running digest is
  months old, and no surface says so.

This is a *display* gap, and display-only runtime facts are squarely in
skipper's scope ([ADR-0027](0027-live-stack-health-in-ui.md) live health,
ADR-0053 running versions,
[[project-scope-visualization-not-trigger]]). The Stacks view already answers
"what runs" and "is it healthy"; "is it current" is the missing third of the
same question.

## Decision

**skipper periodically asks each registry what it offers for the images its
stacks run, and reports the answer — in the UI and optionally as a
notification. It acts on nothing.**

### What is checked

Per running service (from `running_images`, ADR-0053), two questions — mirroring
the two forms the version chip already distinguishes:

1. **Newer tag** (version-shaped tags only): list the repository's tags and
   compare against the running tag, considering **only tags of the same shape**
   — same prefix (`v` or none), same number of dot-separated numeric
   components, same suffix (`6.2-alpine` is only compared against
   `*-alpine`). The highest such tag, if newer, is advertised:
   `1.22.3 ⇡ 1.22.6`. Cross-shape suggestions (`latest`, a different variant
   suffix, more/fewer components) are never made — a wrong upgrade hint is
   worse than none.
2. **Moved digest** (every tag, and the only check for non-version tags like
   `latest`): fetch the manifest digest for the *running* tag and compare it
   against the local image's repo digest (`docker image inspect`,
   `RepoDigests`). A mismatch is advertised as *rebuilt*: `v3.1 ⇡ rebuilt` —
   the same-tag form the Deploys column already renders as `tag ↻`.

### How it talks to registries

A minimal client in `internal/registry` speaking the Registry HTTP API v2:
token auth (anonymous for public images; credentials from the host's docker
config for private ones — the same credentials its pulls already use),
`GET /v2/<repo>/tags/list` for question 1, `HEAD /v2/<repo>/manifests/<tag>`
for question 2. Behind a consumer-side interface so tests inject a fake, per
the `Runner` discipline ([ADR-0003](0003-runner-abstraction-and-fake-based-tests.md)).

This deliberately departs from ADR-0030's sketch of registry reads through the
`Runner` seam (`docker manifest inspect` / skopeo / crane): the docker CLI has
no tag-listing command at all, and skopeo/crane are external tools skipper
cannot assume on a host. The sketch's constraint served an *acting* watcher,
where reusing docker's code path mattered for safety; a read-only reporter
needs two GET/HEAD-shaped requests, and a scoped client is less surface than a
new binary dependency. Notably, `HEAD` manifest requests do not count against
Docker Hub's pull rate limit, and the tag list is not a pull either — a check
cycle spends no pull tokens.

The local half (`docker image inspect` for repo digests) does go through the
existing `Runner`.

### When it runs

Its own headless ticker, like reconcile and healthwatch — **not** UI-gated,
because the notification below must fire with no browser open. Additionally,
after every deploy run **whose running images changed**, the check re-runs
immediately: a deploy that applied an update clears its marker now, not at the
next tick up to a full interval later — a marker claiming an update the host
already runs would be exactly the false signal this feature must never give.
The frequent no-op reconcile runs skip that nudge without registry traffic
(the running-images comparison is local). Config, in its own `internal/config`
file per the feature-area layout:

```yaml
update_check:
  interval_seconds: 21600   # default 6h; 0 disables the feature
  notify: false             # opt-in: one notification when an update first appears
```

Per-stack opt-out: `update_check: false` in the stack's overrides (for a stack
whose floating tag is *meant* to lag, or whose registry is unreachable).
Like `self_heal`/`autosync`/`rollback`, the toggle is runtime policy and is
**not** part of `ConfigHash` — flipping it must not redeploy.

Default **on**: the feature is read-only, spends no pull tokens, and the check
interval is measured in hours. The one cost of the default is outbound
requests to the registries the host already pulls from; a deployment that must
not phone registries outside a deploy sets `interval_seconds: 0`.

### Where the answer lives

- **Runtime-only snapshot**, held like live health and carried on the existing
  `stacks` SSE snapshot — never in a hashed input, never in the pull decision
  (Invariants 2 and 5 untouched). A check failure degrades to "no marker" and
  a log line; it never invents staleness and never blocks anything.
- **Peers**: a peer includes its snapshot's update info in `/api/v1/snapshot`,
  so the primary's fan-in ([ADR-0048](0048-multi-host-federated-ui.md)) shows
  peer stacks' updates with no extra endpoint.
- **UI** (mockup approved, variant A): the existing version chip
  (`.tag-delta`) gains an amber `⇡ <new>` / `⇡ rebuilt` token and a faint
  queued tint — queued amber because the app already uses it for "waiting to
  be applied", which an available update is. No new badge, pill, or panel: the
  roster row's chip and the containers panel's per-service chips carry the
  marker, and the containers panel head gains a muted
  `⇡ N updates · registry check 12m ago` in its existing meta slot. Full
  references and the check time live in the chip `title`, like every chip.
- **Notification** (opt-in `notify: true` and a sink configured): one message
  through the existing pipe when a service first flips to an advertised version —
  "gitea: 1.22.6 available (running 1.22.3)". Deduped per
  stack/service/advertised identity, and the dedup map is persisted in its own
  small state file beside `state.yaml` (the healthwatch precedent —
  `state.yaml` itself is written under the deploy-run lock, which an
  out-of-run checker must not contend with), so a restart — skipper
  self-updates routinely — does not re-send every standing update. An errored
  check keeps its dedup record: a registry outage must not re-page on
  recovery.

### What it never does

No pull, no deploy, no button. Applying an update stays a git commit — a tag
bump by hand or a Renovate PR — flowing through the unchanged
webhook/reconcile → hash → pull → up path. This ADR adds the *knowing*, not
the *doing*; shape B stays rejected.

## Consequences

- The mutable-tag blind spot becomes visible without Renovate, and pinned-tag
  hosts get a release radar in the pane they already watch. On a
  Renovate-covered repo the markers are short-lived (the PR lands, the chip
  clears on the next check after the deploy) — the two features compose rather
  than compete.
- skipper gains an outbound HTTP surface toward registries: new failure modes
  (auth, rate limits, air-gapped hosts) confined to one package, degrading to
  an absent marker plus a log line. `interval_seconds: 0` removes the surface
  entirely.
- Tag-shape matching is deliberately conservative, so some real updates go
  unadvertised (a repo that switches from `1.22` to `1.22.0`-style tags, a
  date-tagged image). The digest check still covers those once the running
  tag itself is republished.
- Multi-arch is handled by comparing against the manifest-list digest (what
  `RepoDigests` records); no per-arch resolution is attempted.
- Stacks with nothing running (parked, disabled, never deployed) and services
  without a recorded running image are skipped — there is nothing truthful to
  compare.
- The state dir gains one small report-only file (the notification dedup map).
- The UI change is a chip state, not a new element — the
  [ui-design-concept](../ui-design-concept.md) budget questions are answered
  with "the version chip carries it" and "nothing comes off, nothing was
  added".

## Alternatives considered

- **Stay ADR-0030-pure: run Renovate, need no in-skipper check.** Right for
  acting, insufficient for seeing: it presumes a forge Renovate watches and
  covers every image, and even then the *UI* stays silent — the answer lives
  in a PR list, not the pane that shows what runs. Rejected as the only
  answer, kept as the acting half.
- **Registry reads via `Runner` + skopeo/crane.** Keeps the no-HTTP-client
  line, but adds a binary dependency skipper cannot assume, still cannot list
  tags via the docker CLI, and swaps a scoped, fake-injectable client for
  argv-parsing of a third-party tool. Rejected.
- **Digest-only check** (skip tag listing). Half the network surface, but it
  misses the newer-tag case — precisely the useful half for the manual-pin
  homelab this feature serves. Rejected; the conservative shape-matching
  bounds the risk of the tag half.
- **A sidecar (diun/watchtower monitor-only).** Proven tools, but another
  service to run, configure, and wire to a second notification pipe — against
  the one-pane-of-glass point of the UI, and blind to skipper's
  per-service running identity. Rejected.
- **Surface it as a deploy trigger or UI button.** Rejected —
  [[project-scope-visualization-not-trigger]] and ADR-0030 shape B.

## Amendment (2026-08-02): `notify` defaults to false — the UI is the surface

The original default sent a notification for every newly appearing update.
In practice that is the wrong channel for this signal: an available update is
a standing state to look at when convenient, not an event that warrants a
push. Its cadence is set by upstream release schedules, not by anything on the
host, so across a homelab's worth of stacks it produces a steady trickle of
messages that no one acts on immediately — and that noise erodes the value of
the channel that also carries `failed`, `rolled_back` and `heal_exhausted`,
which *do* demand attention now.

`UpdateCheckNotify()` therefore defaults to **false**: the check reports in
the Stacks view (the version chip's `⇡` marker) and nowhere else. Setting
`update_check.notify: true` opts back in; everything about the message, the
dedup identity and its persistence in `update-check.yaml` is unchanged — only
the default flips. The dedup file is still written whenever notifications are
on, and is simply unused when they are not.
