# Feature Spec: Traefik App Links

Status: implemented (ADR-0041)
Date: 2026-07-19

## Goal

Close another small ArgoCD-style gap: skipper already shows *that* a stack is
healthy, but not *where to go look at it*. If a stack's compose services carry
Traefik routing labels, show a small link icon in the Stacks view that opens
the app directly — no manual bookmarking, no guessing the hostname from the
compose file.

Non-goals: reverse-proxy configuration or editing, any proxy other than
Traefik (nginx/Caddy labels are a different shape entirely — homelab-scoped
like the rest of skipper), per-service icons (stack-level only, one icon per
row), URL health-checking (a link that 404s is still shown — this is
navigation, not a probe).

## Detection

Read live via `docker inspect`, not the compose file's static YAML —
deliberately, so labels using env-var interpolation (`${DOMAIN}` etc., loaded
from `env_files`/`vars_file`) resolve to the exact value the running
container has, the same value Traefik itself is routing on. This is a
read-only display feature, not change detection, so it does not need to
follow Invariant 1's "compose file from the repo clone" rule — that invariant
is about what triggers a redeploy, not what the UI is allowed to look at.

Per stack:

1. `docker ps --filter label=com.docker.compose.project=<name> --format '{{.ID}}'`
   — reuses the same project-label filter `internal/orphans` already uses to
   enumerate a stack's containers.
2. `docker inspect --format '{{json .Config.Labels}}' <ids...>` — one real JSON
   object per container (unlike `docker ps`'s `{{.Labels}}`, which is a
   comma-joined string and unsafe to split on arbitrary label values).
3. For each container, only if it carries `traefik.enable=true` (Traefik's own
   opt-in convention — homelab setups typically run with
   `exposedByDefault=false`, so a missing label means "not exposed", not
   "unset"), scan its labels for keys matching
   `traefik.http.routers.<router>.rule` and extract every `Host(`...`)` term
   from the rule value via regex. Rules can combine `Host()` with
   `PathPrefix()`/`Headers()` etc. via `&&`, or list several hosts via `||` —
   only the `Host()` literals are pulled out. `HostRegexp(...)` and anything
   that fails to parse are skipped (not shown), since they are not a single
   clickable hostname.
4. Hosts are collected across every service/container in the stack, deduped,
   sorted alphabetically — stable order for the UI.

Scheme is always `https://` — the homelab default (Traefik terminates TLS
externally). No per-host scheme detection; out of scope for v1.

## Package layout

New package `internal/applink`, modeled directly on `internal/orphans`:

- `Detector` owns the docker calls and the label-parsing above; `Config{
  Outputter, Managed func() []string /* stack names */, Publish func(Snapshot)
  }`.
- **No independent ticker.** Checked against `cmd/skipper/main.go`:
  `orphans.Detector` doesn't own a timer either — it's driven by the health
  poller's `OnSnapshot` hook, so orphan detection rides the existing
  health-poll cadence instead of spinning up a second goroutine. `applink`
  does the same: registered as another `OnSnapshot` consumer, own `docker`
  calls, shared tick. Keeps `internal/health` itself untouched (its doc
  comment scopes it to runtime health only) while avoiding a third unrelated
  poll loop next to health's and reconcile's.
- `Snapshot` is `map[stackName][]string` (hostnames). Folds into the existing
  `stacks` SSE feed the same way the orphans snapshot does — no new dedicated
  endpoint.
- Refresh on deploy completion too (not just the next poll tick): hook into
  the existing deploy-completion path so a stack whose Traefik labels changed
  in the same push shows the new hostname immediately, not after the next
  poll interval.

## UI integration

See mockup: a small icon button joins `.roster-stack`'s existing
jump/logs-button row (after the compass and logs buttons), same 20×18px
bordered frame, muted → accent on hover. No new grid column — the roster's
`Stack · Status · Last deploy · Commit` layout is untouched.

- **Exactly one hostname**: the button is a plain `<a target="_blank"
  rel="noopener">` — click opens the app directly.
- **More than one hostname**: the button toggles a small popover listing each
  hostname as its own link.
- **No hostname** (no Traefik labels, stack never deployed, or parked/
  `disabled: true`): no icon renders at all. Never a disabled/ghost state —
  nothing to click means nothing shown.

## Config

None planned. Detection is inert wherever Traefik isn't used (no labels found
→ no icon), so there's no need for an opt-in/opt-out toggle — consistent with
other automatic, harmless-when-absent features (icons, health pill).

## Open questions

- Whether the batched `docker inspect` call per poll tick is noticeable on a
  host with many stacks — currently assumed negligible, same order of
  magnitude as the existing health/orphans polling; not yet measured on a
  large host.

See [ADR-0041](adr/0041-traefik-app-link-detection.md) for the implementation
decisions.
