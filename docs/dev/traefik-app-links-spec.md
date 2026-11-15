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
navigation, not a probe), any Traefik provider other than Docker labels —
routing declared via the file/dynamic provider, a Kubernetes Ingress, or any
other non-Docker provider carries no container labels, so it is invisible to
this detection and simply yields no icon, even though Traefik is genuinely
routing to it.

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

## Traefik file-provider support: decided against (2026-08-05)

v1 only detects routes declared via Traefik's **Docker provider** (container
labels) — see the non-goal above. The file provider (dynamic config files,
typically under `/etc/traefik/dynamic`) is not covered; first discussed
2026-07-19 and deferred, then **closed as decided-against on 2026-08-05**
after checking the design against a real homelab deployment:

- Every file-provider route there pointed at an **IP-literal backend on a
  different host** (a native service on another machine, or another skipper
  instance) — none at a compose container the skipper host manages. The
  generic attribution below (backend URL → container/service name) would
  therefore have matched nothing and rendered zero additional icons. That is
  the expected shape of file-provider use: routes to local containers get
  Docker labels for free, so the file provider tends to carry exactly the
  routes that have no local stack to attribute to.
- The access path did turn out cheaper than originally estimated — the
  dynamic dir was host-bind-mounted, and both the Traefik container and the
  host-side path are discoverable generically via `docker inspect` (a
  `--providers.file.directory=…` flag in `.Config.Cmd`/`.Config.Env`, mapped
  through `.Mounts`) — no `docker exec` needed. That lowers the cost, but
  doesn't change the zero-value attribution result.

The design below is kept for reference in case a setup with file-provider
routes to *local* containers ever makes it worth reopening.

The hard problem isn't reading the files — it's **attribution**: a
docker-label router lives on the very container it routes to, so the stack is
free (via `working_dir`). A file-provider router has no such anchor; a naming
convention (router name, or file name, == stack name) is fragile and was
explicitly rejected — "die Lösung soll generisch sein". The one piece of
information that's *actually* required to be correct (or Traefik itself
couldn't route) is the router's backing **service address**
(`http.services.<name>.loadBalancer.servers[].url`) — so a generic solution
means resolving *that* back to a container, not trusting any name:

1. Locate the Traefik container generically via `docker inspect`: the
   container whose `.Config.Cmd`/`.Config.Env` carries
   `--providers.file.directory=…`.
2. Read the dynamic config files (YAML only — Traefik also accepts TOML, out
   of scope): resolve the directory through the container's `.Mounts` to its
   host-side path and read it directly; only a setup with no host bind-mount
   would need a `docker exec`/`docker cp` fallback.
3. Parse every `http.routers.*.rule` (reuse `extractHosts`) and resolve its
   `service` to that service's backend server URL(s).
4. Match the backend URL's host component against known container
   names/compose-service names (from the existing `docker ps` listing) to
   find the owning stack — generic, no naming convention, but only works when
   the backend address is docker-network-resolvable (a container/service
   name). An IP-literal or external-DNS backend simply won't match — no
   error, just no icon, same graceful degradation as today.

Scope this adds beyond the shipped v1: a new host-filesystem access path
resolved through container mounts, a Traefik dynamic-config YAML parser, and
backend-URL-to-container matching (with real edge cases: multiple servers
per service, load-balancer weights, TCP/UDP routers to ignore). Its own ADR
amendment and spec pass, not a quick follow-up — and per the decision above,
only worth it for a deployment whose file-provider routes actually target
local containers.

See [ADR-0041](adr/0041-traefik-app-link-detection.md) for the implementation
decisions.
