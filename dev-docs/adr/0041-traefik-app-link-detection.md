# ADR-0041: Traefik app-link detection (live labels, health-poll cadence)

Status: accepted
Date: 2026-07-19

## Context

skipper shows that a stack is healthy, but not where to go look at it — a
homelab operator still has to remember or bookmark each app's hostname
separately. Most stacks behind Traefik already declare their routing in
`traefik.http.routers.*.rule` compose labels, so the hostname is discoverable
without any new user-facing config. See `dev-docs/traefik-app-links-spec.md`
for the full spec and the approved UI mockup.

Non-goals: reverse-proxy configuration or editing, any proxy other than
Traefik, per-service icons (stack-level only), URL health-checking (a link
that 404s is still shown — navigation, not a probe).

## Decision

### Detection is live (docker inspect), not a static compose-file parse

Unlike change detection (Invariant 1: always the repo clone), this is a
read-only display feature, so it reads the *running* containers' labels via
`docker inspect --format '{{json .Config.Labels}}'`. That resolves labels
using env-var interpolation (`${DOMAIN}` from `env_files`/`vars_file`) to the
exact value Traefik itself is routing on — a static YAML parse would only see
the unresolved template.

### One `docker ps` + one batched `docker inspect`, not N per-stack calls

A single `docker ps --filter label=com.docker.compose.project --format
'{{.ID}}\t{{.Label "com.docker.compose.project.working_dir"}}'` lists every
running compose container's ID and working_dir in one call (no `-a`: a
stopped container has nothing Traefik is routing to, so a stopped stack
simply yields no hosts). Then one batched `docker inspect <all ids...>
--format '{{json .Config.Labels}}'` returns each container's full label map —
a real JSON object, unlike `docker ps`'s comma-joined `{{.Labels}}` string,
which cannot be safely split on arbitrary label values (Traefik rule labels
contain commas and parens).

### Identity is the working_dir label, matching orphan detection

Containers are grouped and matched to a stack by
`com.docker.compose.project.working_dir`, the same identity ADR-0036 (orphan
detection) uses — stable across a rollback's `/tmp` compose file (Invariant
3), unlike the compose project name, which is not guaranteed to equal the
stack name.

### Host extraction: regex over Host() calls, gated by traefik.enable

A container's labels are only scanned when it carries `traefik.enable=true` —
Traefik's own opt-in convention, since homelab setups typically run with
`exposedByDefault=false`, so a missing label means "not exposed", not
"unset". Every `traefik.http.routers.<router>.rule` label is scanned for
`Host(...)` calls; every backtick-quoted literal inside is pulled out,
handling both Traefik v2 (`Host(\`a\`) || Host(\`b\`)`) and v3
(`Host(\`a\`,\`b\`)`) shapes the same way. `HostRegexp(...)` and any other
matcher (`PathPrefix`, `Headers`, ...) combined via `&&`/`||` are ignored — a
regexp is not a single clickable hostname. Hosts are deduped and sorted per
stack.

### Wiring: its own package, riding the health-poll cadence — no new timer

New package `internal/applink`, mirroring `internal/orphans`: a `Detector`
behind an `Outputter` (`command.Runner`), with `Detect` driven externally
rather than owning a timer. Checked against `cmd/skipper/main.go` before
building this: `orphans.Detector` doesn't own a timer either — it's invoked
from the health poller's `OnSnapshot` hook. `applink` registers as another
`OnSnapshot` consumer, gated the same way (`stateB.HasSubscribers()`) so it
does no docker work while no UI client is watching. This keeps
`internal/health` itself unchanged (its own scope is runtime health only) and
avoids a third independent poll loop alongside health's and reconcile's. A
docker failure (either call) leaves the last snapshot in place rather than
blanking every icon.

Because `healthPoller.Poll()` already triggers every `OnSnapshot` consumer,
the existing "refresh right after a deploy" and "refresh on client connect"
call sites (`deploy.Config.PostRunHook`, `stateSnapshot.collect()`)
automatically refresh app-links too — no extra hook needed.

### UI: icon joins the existing per-row button row, no new column

The icon sits inline in `.roster-stack`, after the jump/logs buttons — same
20×18px bordered frame, muted → accent on hover — so the roster's `Stack ·
Status · Last deploy · Commit` grid is untouched. One hostname is a plain
`<a target="_blank">`; several open a small popover (`<span class="link-wrap">`
wrapping a `<button>` trigger and a sibling `<div class="link-pop">` — not a
button-containing-anchors, which is invalid HTML and can corrupt parsing).
Zero hostnames renders nothing: never a disabled/ghost icon. The `app_links`
SSE snapshot patches rows in place (`updateAppLinks()`) rather than triggering
a full roster re-render, since it updates on the health-poll cadence — far
more often than the `stacks` snapshot — and a full rebuild would drop any
open audit/health panel.

## Consequences

- New `internal/applink` package (`Detector`, `Config`, `Snapshot`) plus a
  `StateAppLinks` ("app_links") SSE event, following the exact shape of
  `internal/orphans`/`StateOrphans`.
- Two extra docker calls per health-poll tick while a UI client is connected
  (one `ps`, one batched `inspect`) — same order of magnitude as the existing
  health and orphan-detection polling; not expected to matter at homelab
  scale, but worth a quick check if a host ever carries many stacks.
- No new config surface: detection is inert (no labels found → no icon)
  wherever Traefik isn't used, so there is no opt-in/opt-out toggle to
  maintain.
- Traefik-specific by design (dev-docs/traefik-app-links-spec.md's
  non-goals) — a different reverse proxy's labels are simply never matched,
  degrading gracefully to "no icon" rather than an error.
