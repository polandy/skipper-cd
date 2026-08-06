# TODO

- **UI branding: adopt the homelab "Flake" identity.** Apply the mark (lambda in a
  hexagon) to the web UI: favicon and header logo, plus the accent palette where it
  fits the existing design (Nix blue `#5277C3`/`#7EBAE4`; LED green `#3DDC84` dark /
  `#1FA864` light as accent; amber/red remain status-only). Source SVGs and usage
  rules live in the nixos repo under `branding/` (see `branding/README.md` there);
  use `favicon.svg` (heavier stroke) for anything rendered at 16-24 px. Per repo
  convention, update `internal/ui/UI_SPEC.md` first, then implement.

- **Fast-forward the `project_directory` checkout before stack deploys.** Relative
  bind mounts in a compose file resolve against `--project-directory`, not against
  the clone the compose file is read from (Invariant 1) — so on a host where
  `project_directory_base` points at a separate checkout, every `./config`,
  `./dashboards` or `./restic-hooks` mount is served from that checkout, and
  nothing keeps it current. skipper pulls its own clone, rebuilds from it, and then
  deploys with `--project-directory <base>/<name>`, at which point the mounted
  content can be arbitrarily old. Observed on the homelab nuc (2026-08-02), where
  `project_directory_base: /etc/nixos/modules` is also the operator's development
  checkout: `docker inspect monitoring-grafana` showed its dashboards mounted from
  `/etc/nixos/modules/monitoring/grafana-data/dashboards`, which only advances on a
  manual `git pull`. The failure is invisible — the container runs, the metric
  exists, the panel just answers with an older query — and it stays hidden while
  changes are authored on the host itself, since the working copy is then ahead
  anyway. It needs a change merged from elsewhere (a web-UI merge, or an automerged
  Renovate image bump) to bite.

  **Ordering is the whole point of doing this in skipper rather than beside it.**
  A pull *after* a successful deploy — the obvious first instinct — repairs only the
  *next* deploy: content read once at container start (Grafana provisioning,
  `prometheus.yml`) has already been read from the stale file by the container that
  was just recreated, and the new file is picked up whenever that service happens to
  restart next. Equally, an external watcher (a systemd path unit on the clone's
  `HEAD`, say) races the deploy it is trying to precede. Only skipper knows when the
  deploy runs, so only skipper can reliably put the fast-forward in front of it.

  Sketch: an opt-in config key (`project_directory_sync: true`, or a small section
  if a remote/branch ever needs naming) that, once per run before the stack phase,
  fast-forwards the `project_directory_base` checkout. Constraints that shape it:

  - **`--ff-only`, never a merge or reset.** The checkout is a working copy someone
    edits; diverged history must fail loudly, not be rewritten.
  - **Skip a dirty tree**, and say so in the run's events rather than failing the
    deploy — an operator mid-edit must not have a deploy blocked, nor their work
    touched.
  - **Not a deploy input.** Like `self_heal`/`rollback`, this is host policy: it must
    stay out of `ConfigHash` (Invariant 2), or toggling it redeploys everything.
  - **Only when a base is configured**, and a no-op when the base resolves inside
    the clone itself (the common single-checkout case) — no second pull there.
  - Failure of the fast-forward should surface as a run-level event, not a per-stack
    one; whether it should also abort the stack phase (as a failed `nixos_rebuild`
    does, Invariant 4) is the open question — leaning no, since a stale mount is
    degraded rather than wrong.

  Per repo convention this is a significant enough change to want a `dev-docs/adr/`
  record, and it touches `docs/nixos.md` (which documents `project_directory`) and
  `docs/configuration.md`. Test-first, with the argv asserted through the
  `recordingRunner` fakes as usual — plus the dirty-tree and non-fast-forward
  refusals, which are the safety paths. Tracked on the consuming side in the nixos
  repo's `docs/TODO.md` under Medium Priority.
