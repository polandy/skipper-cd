# ADR-0069: The health poll reads every stack's container state in two docker calls

Date: 2026-11-12

## Status

Accepted. Amends [ADR-0027](0027-live-stack-health-in-ui.md) (adopts the
batched alternative it deferred, without its `Status` scraping).

## Context

The runtime health poll (ADR-0027) ran `docker compose ps --format json --all`
once per stack on every tick. Each of those is a compose process plus a
container list and one inspect per container on the daemon side. With the
watchdog and self-heal making the poll headless (ADR-0029, ADR-0031), it runs
every 30 s whether a browser is open or not; on a host with 29 stacks that cost
about 18 % of a core. ADR-0027 already named one batched `docker ps` as the
lever and deferred it only because `docker ps` reports health as free text
inside `Status`.

## Decision

One poll makes two docker calls, whatever the stack count:

1. `docker ps --all --no-trunc --filter label=com.docker.compose.project.working_dir
   --filter label=com.docker.compose.oneoff=False --format '{{.ID}}\t{{.Image}}'`
   lists every compose service container, stopped ones included and one-off
   `compose run` containers excluded — the set `compose ps --all` shows. The
   image is taken from this listing because it is the field compose ps reports
   (the image ID once the tag has moved to a newer image), so the roster's
   running version does not change.
2. One `docker inspect --format …` over those IDs reads the structured fields:
   the working_dir and service labels, the name, `State.Status`,
   `State.Health.Status` and `State.ExitCode`. No `Status` text is parsed.

The result is shaped exactly as compose ps shaped it: health only for a running
container, the exit code only for an exited or dead one, the leading `/` dropped
from the name, and each project's containers sorted by name. The rollup,
on-demand marking and SSE snapshot are unchanged.

Containers are matched to a stack by the `com.docker.compose.project.working_dir`
label: the stack's `project_directory` when it has one, else the directory of its
compose file in the clone — the value the deploy path's `--project-directory`
(or compose's own default) stamps on the container. Orphan detection and the
app-link resolver already key projects by this label.

Failure handling:

- The listing fails, or inspect fails without printing anything: every stack
  reports `unknown` for that tick, as one failed per-stack call did for its stack
  before. Malformed output is treated the same.
- Inspect fails but printed rows: a container was removed between the two calls.
  The rows are used and the missing container is simply absent, which is what a
  compose ps a moment later would have shown.
- No containers: no inspect call. No stacks: no docker call.

Plain docker does not interpolate `${VAR}`, so the poll no longer needs the
`vars_file` environment; `NewShellRunnerWithOutputEnv` and the poller's env
plumbing are removed.

## Consequences

- Daemon and process work per tick no longer grows with the stack count beyond
  one inspect row per container.
- One daemon hiccup now turns every stack `unknown` for a tick instead of one.
  The watchdog and self-heal already treat `unknown` as "no verdict", so this
  cannot raise an alert or a heal; it only flickers the pill.
- Two stacks deployed into the same project directory would share one set of
  containers. That is not a supported layout (they would be one compose project
  already), so it is not guarded.
- The e2e and preview docker stubs answer the two calls from a per-stack health
  table alongside the compose ps JSON the deploy path's running-image read still
  uses.

## Alternatives considered

- **Keep one `docker compose ps` per stack.** The cost this ADR removes.
- **One `docker ps` and parse `Status`** (ADR-0027's deferred option). Health
  appears only as `(healthy)` / `(health: starting)` inside free text, and the
  exit code as `Exited (1) 3 minutes ago`; the inspect call costs one process and
  reads both as fields.
- **Match by `com.docker.compose.project`.** The project name depends on a
  compose file's `name:`, `COMPOSE_PROJECT_NAME` and normalisation rules skipper
  would have to reimplement; the working_dir is what skipper itself passes.
- **Subscribe to `docker events`.** No polling at all, but a long-lived stream
  through the run-to-completion `Runner`, plus a resync path for missed events —
  more moving parts than two cheap calls per interval.
