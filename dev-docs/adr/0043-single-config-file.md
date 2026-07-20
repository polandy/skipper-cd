# ADR-0043: A single configuration file (fold per-stack overrides into the host config)

Status: accepted
Date: 2026-07-20

## Context

skipper currently has **two** configuration files that can shape a deploy:

1. The **host config** (`skipper.yml`, loaded via `-config`, default
   `/etc/skipper/skipper.yml`). Holds bootstrap/host concerns: `repo_url`,
   `stacks_base_dir`, `webhook_secret`, ports, notifications, … In the homelab
   it is rendered from `/etc/nixos` by the NixOS module. Always required.
2. The **repo override file** (`<stacks_base_dir>/skipper.yaml` in the deploy
   clone, ADR-0034 + its 2026-07-18 amendment). Optional. In stack-discovery
   mode it carries the per-stack overrides — `health_check`, `hooks`, `rollout`,
   `depends_on`, `env_files`, `disabled`, … — keyed by stack name.

The two are near-name-twins (`skipper.yml` vs `skipper.yaml`), which is a
standing source of confusion. More importantly, having per-stack config in a
*second* file was a deliberate ADR-0034 choice so that a stack's config could
change with a plain git push (no host edit / rebuild). In practice, for the
NixOS homelab this ADR targets, the operator wants **one** declarative source of
truth: everything that describes the host — including which stacks get which
overrides — should live in the one config that nix renders, reviewed and applied
the same way as the rest of the system.

Stack *membership* via directory discovery (ADR-0034 variant A) is not in
question and stays: a directory under `stacks_base_dir` with a
`docker-compose.yml` is a stack. Only the *location of the overrides* is.

## Options considered

**0. Status quo (ADR-0034).** Two files: host bootstrap + repo override file.
Per-stack config edits are a git push, no rebuild.
- Strongest argument for: change a stack's hooks/health gate without touching
  the host or rebuilding — config arrives atomically with the content it
  describes.
- Cost: two config files with confusingly similar names; per-stack config lives
  outside the host's declarative source of truth; multi-host monorepos need the
  per-`stacks_base_dir` anchoring hack (the 2026-07-18 amendment) to keep each
  host's overrides disjoint.

**A. One config file — overrides fold into the host config (this ADR).** There
is exactly one skipper config file: the host config, renamed to `skipper.yaml`
(`.yml` kept as an accepted alias). Under `stack_discovery: true` it may carry an
**optional** `stacks:` map — the same override shape as today's repo file, keyed
by stack name — that configures discovered stacks. The separate repo override
file is removed.
- Strongest argument for: one declarative source of truth; the name-twin
  confusion is gone; in a multi-host monorepo each host's config naturally holds
  only its own stacks' overrides, so the per-`stacks_base_dir` anchoring hack is
  no longer needed.
- Cost: a per-stack config edit is now a host-config edit — in the homelab, a
  `/etc/nixos` change + `nixos-rebuild` (which restarts skipper), not a plain git
  push. The UI can no longer show a git diff of the offending repo file for a
  config-driven redeploy (the config now lives host-side, outside the deploy
  repo).

**B. One file, but in the repo.** Put the single `skipper.yaml` (incl. per-stack
overrides) into the deploy repo. Rejected: bootstrap concerns can't live there —
`repo_url` is self-referential (needed to clone the repo the file is in) and
`webhook_secret` must not be committed to git. The one file that has these is
necessarily host-side.

**C. Allow the host `stacks:` map *and* still read the repo file.** Keep both
sources, host wins. Rejected: this is *more* configuration surface, not less —
the opposite of the one-file goal — and reintroduces the name-twin.

## Decision

**Option A.** One skipper configuration file. Stack membership is discovered from
`stacks_base_dir`; per-stack overrides, when needed, live in that one file's
optional `stacks:` map. The in-repo override file (ADR-0034) is removed.

This **supersedes the override-file mechanism of ADR-0034** (its 2026-07-18
amendment in particular); ADR-0034's directory-discovery-of-membership decision
stands. ADR-0034's status is updated to "partially superseded by 0043".

Concretely:

- Relax the current invariant that `stack_discovery: true` is *mutually
  exclusive* with a host `stacks:` list. Under discovery, `stacks:` becomes an
  **optional override map** keyed by stack name (today's `repoStackOverride`
  shape), not a membership list. A name with no discovered directory is an
  entry-level error, same as the repo file gives today.
- Delete `LoadRepoStacks`'s reading of `<stacks_base_dir>/skipper.yaml`; the
  override map comes from the already-loaded host config. Directory discovery of
  the set is unchanged.
- Rename the default config file to `skipper.yaml` (accept `skipper.yml` as an
  alias) — **follow-up**, cosmetic and downstream-only (the NixOS module passes
  `-config` explicitly), kept out of the mechanism change.
- The NixOS module gains the per-stack override options under discovery (today
  they apply only when stacks are listed explicitly); it renders them into the one config.
- The in-repo-file error snippets (the `>`-marked excerpt ADR-0034 added) are
  dropped: the config is host-side now, so there is no repo file to excerpt.
  Entry-level errors still name the stack and the problem.
- **Clean break, no deprecation fallback.** The in-repo override file is simply
  no longer read. To avoid a silent reset to defaults if the overrides are not
  moved before the upgrade, a leftover `<stacks_base_dir>/skipper.yaml` after the
  upgrade **fails the stack phase loudly** (reserved `_config` key: "repo
  skipper.yaml is no longer read — move its overrides into the host config"),
  rather than being ignored. Justified because the only users are two
  single-admin hosts migrated atomically; a deprecation window exists for
  unknown third parties, of which there are none, and it would only add
  dual-source precedence logic.
- **Per-stack `autosync` override becomes available.** ADR-0034 excluded it
  because the autosync controller's per-stack config is fixed at startup from
  `cfg.Stacks`, which is empty in discovery mode. With overrides now in the
  startup host config, the controller reads the override map at startup as it
  does when the stacks are listed in the host config, so the blocker dissolves. Autosync stays **on by default
  for every stack**; a per-stack `autosync: false` in the override map opts a
  single stack out (the existing `*bool` inherit semantics). This does **not**
  address ADR-0034's *other* v1 limitation — self-heal *activation* still follows
  the global flag, because the stack *set* is still discovered per-sync, not
  known at startup.

## Consequences

- **One source of truth.** Everything that describes the host is in one file
  (nix-rendered in the homelab), reviewed and applied like the rest of the
  system. The `skipper.yml`/`skipper.yaml` twin confusion is gone.
- **Config edits need a host change again.** Changing a stack's hooks /
  health_check / rollout is a host-config edit; in the homelab that is a
  `/etc/nixos` change + `nixos-rebuild`, which restarts skipper. Adding /
  removing a *stack* is still a plain git push (discovery unchanged). This is the
  central trade-off and the reason ADR-0034 chose the repo file — it is
  deliberately reversed here in favour of a single declarative source.
- **Multi-host monorepo gets simpler.** Each host's config holds only its own
  stacks' overrides by construction, so the per-`stacks_base_dir` anchoring hack
  (ADR-0034 amendment) is no longer needed for override isolation. Discovery
  still anchors the *set* at each host's `stacks_base_dir`.
- **ConfigHash / change detection is unchanged in spirit** but re-keyed: the
  per-stack deploy-shaping config still hashes into change detection so an
  override edit redeploys exactly that stack; it is just sourced from the host
  config now. The UI loses the "show the git diff of the changed repo file" for
  config-driven redeploys — the config is no longer a file in the deploy repo.
- **Migration.** nuc and argoneon currently run discovery with committed repo
  `skipper.yaml` overrides (LIVE). Accepting this ADR means moving those
  overrides into each host's NixOS module and deleting the repo files, in one
  coordinated change per host. Until migrated, both hosts keep working on the
  current mechanism.
- **Validation timing is unaffected.** The discovery-time compose/rollout/path
  validation (ADR-0034 amendment 2026-07-20) still runs — it validates the
  discovered stacks against their compose files regardless of where the
  overrides come from.

## Resolved during review (2026-07-20)

- **Clean break** over a deprecation fallback, with a loud leftover-file guard
  (see Decision). Only two single-admin hosts, migrated atomically.
- **Per-stack `autosync` override is enabled** (default on, per-stack
  `autosync: false` opts out). The self-heal-activation limitation is left as-is.
