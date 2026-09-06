# ADR-0060: The project_directory checkout is fast-forwarded before the stack phase

Status: accepted
Date: 2026-09-06

## Context

Invariant 1 keeps two directories apart on purpose. Change detection and the
compose file come from the repo clone; `--project-directory` (ADR-0045) points
at the tree that carries a stack's runtime data, typically derived per stack
from `project_directory_base`.

Compose resolves **every** relative path in a compose file against that project
directory. ADR-0057 closed this for `build:` contexts by pinning them to the
clone. Bind mounts are the other half, and they must *not* be pinned — serving
runtime data out of the clone is exactly what the project directory exists to
avoid. So a stack like

```yaml
services:
  grafana:
    image: grafana/grafana:12.1.1
    volumes:
      - ./grafana-data/dashboards:/etc/grafana/provisioning/dashboards
```

mounts `<project_directory>/grafana-data/dashboards`, and nothing keeps that
content current. Skipper pulls its own clone, optionally rebuilds the host from
it, and then deploys with `--project-directory` at a checkout it has never
touched.

Observed on a homelab host where `project_directory_base` is also the
operator's development checkout: `docker inspect` showed the running Grafana
mounting its dashboards from a tree that only advanced on a manual `git pull`.
The failure is invisible — the container runs, the health gate passes, the
metric exists, the panel just answers with an older query — and it stays hidden
while changes are authored on the host itself, because the working copy is then
ahead anyway. It takes a change merged from elsewhere (a web-UI merge, an
automerged image bump) to bite.

## Decision

**Skipper fast-forwards the project-directory checkout itself, once per run,
before the stack phase.** An opt-in top-level key turns it on:

```yaml
project_directory_base: /srv/modules
project_directory_sync: true
```

The ordering is the whole reason to do this inside skipper rather than beside
it. A pull *after* a successful deploy repairs only the *next* one: content a
container reads once at start — a provisioning directory, a scraper config —
has already been read from the stale file by the container the deploy just
recreated, and the new file waits for whenever that service happens to restart.
An external watcher (a systemd path unit on the clone's `HEAD`) has the mirror
problem: it races the deploy it is trying to precede. Only skipper knows when
the deploy runs.

**It fast-forwards and nothing else.** The sequence reads HEAD, checks the tree,
fetches, resolves the upstream, verifies the ancestry and then runs
`merge --ff-only` onto the exact commit it verified. Three conditions stop it
before a single command writes to the working copy:

- **A dirty tree** — modified *tracked* files. Untracked files are ignored: a
  fast-forward never touches them, and an operator's scratch file must not
  freeze the checkout forever. This check runs before the fetch, so a
  permanently-dirty checkout costs no network round trip per reconcile tick.
- **No upstream** on the checked-out branch.
- **A diverged history** — local commits the upstream does not have. Advancing
  it would be a merge or a reset, and the checkout is a working copy someone
  edits.

**A refusal never aborts the run.** A stale mount is degraded, not wrong, and an
operator mid-edit must not have their host's deploys blocked — unlike a failed
`nixos_rebuild` (Invariant 4), where nothing can be said about the host config
at all. The stacks converge as usual against the content the checkout has.

**A refusal is a standing condition, not an event.** It is reported under the
reserved pseudo-stack `_project_dir` as a `failed` event when it appears and
again only when its message changes (ADR-0055), while the gauge
`skipper_project_dir_sync_error` carries it for as long as it lasts. The
messages are deliberately free of the modified paths and the drifting SHAs that
would otherwise re-announce the same condition on every save. A *successful*
fast-forward emits nothing: it is plumbing, exactly like the clone sync at the
top of every run, and the log line records the commits it moved between.

**It is host policy, not a deploy input.** Like `self_heal` and `rollback`,
`project_directory_sync` is a top-level runtime setting and never reaches a
stack's `ConfigHash` (Invariant 2) — toggling it redeploys nothing.

The phase is wired only when it has work: `project_directory_sync: true`
without `project_directory_base` is rejected at load (there would be nothing to
fast-forward, and silently ignoring the key would leave the operator believing
stale mounts were being kept current), and a base *inside* the repo clone
resolves to no phase at all — every sync already resets that tree.

## Consequences

- **Content a stack mounts is as current as the compose file that mounts it**,
  on the deploy that applies the change rather than the one after it.
- **Skipper writes to a directory it does not own.** Narrowly: only a
  fast-forward, only onto a verified-ancestor commit, only on a clean tree, and
  only when the key opts in. Off by default for exactly this reason.
- **A permanently-dirty checkout means the feature does nothing** — by design.
  It says so once and keeps the gauge raised, so "nothing happens" is visible
  rather than assumed.
- **One `git status` and, when the tree is clean, one `git fetch` per run** on
  the reconcile cadence.
- **A third pseudo-stack.** `_project_dir` joins `_nixos` and `_config`; the
  reserved set is now one predicate (`config.IsReservedStackName`,
  `isPseudoStack` in the UI) rather than three open-coded comparisons, which
  also gives `_config` the row treatment it always should have had — no
  container-logs button, no jump to a roster it has no entry in.

## Alternatives considered

- **Pin bind mounts to the clone, as ADR-0057 does for build contexts.**
  Symmetrical and needs no git write at all, but it defeats the point of
  `project_directory`: runtime data (databases, uploads, generated state) would
  land inside a tree skipper `reset --hard`s on every sync.
- **Pull after the deploy instead of before.** Cheaper to reason about — the
  deploy is done, nothing races it — and wrong for the case that matters: the
  container that was just recreated already read the stale file.
- **An external watcher (systemd path unit, cron, a `git pull` in a hook).**
  Keeps skipper out of a foreign checkout, but nothing sequences it against the
  deploy, so it fixes the mount some fraction of the time and the failure stays
  invisible the rest.
- **`git reset --hard`, or a merge, instead of `--ff-only`.** Would make the
  checkout always converge. It also silently discards the work of whoever edits
  that tree — which on the host that motivated this is a person, not a robot.
  Refusing loudly is the whole safety story.
- **Abort the stack phase on a refusal**, as a failed `nixos_rebuild` does.
  Rejected: a rebuild that did not apply invalidates everything downstream,
  while a stale bind mount degrades one stack's content. Blocking every deploy
  on the host because someone left a file open is the larger failure.
- **Treat a dirty tree as a `skipped` event rather than a `failed` one.**
  Semantically closer, but `skipped` is live-only and never enters the history
  (UI_SPEC "Deploy rows"), so the condition would be invisible in the surface
  built to show it. `failed` plus report-once dedup says it exactly once.
