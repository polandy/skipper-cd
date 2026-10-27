# ADR-0065: NixOS change detection compares the host's system derivation

## Status

Accepted. Refines [ADR-0005](0005-nixos-hashes-saved-before-rebuild.md);
[ADR-0015](0015-revert-nix-hashes-on-surviving-rebuild-failure.md) and
[ADR-0025](0025-reconcile-self-restart-interrupted-nixos-rebuild.md) apply
unchanged. Spec: [nixos-drv-change-detection-spec.md](../nixos-drv-change-detection-spec.md).

## Context

The `_nixos` phase decides whether to rebuild by hashing every `*.nix` file
and `flake.lock` in the repository. The question it answers is "did any nix
file change?". The question that matters is "did this host's system change?".
The two differ in both directions.

**Too many rebuilds.** One repository commonly configures several hosts, each
running its own skipper with its own `nixos_rebuild.flake` attribute (the
multi-host setup of ADR-0048). A change to one host's files rebuilds all of
them. A common flake practice makes this worse:
`system.configurationRevision = self.rev` puts the commit into the system, so
every rebuild creates a new generation and boot entry even when nothing else
differs. On a small ARM host, evaluating and building takes minutes.

**Missed rebuilds.** A system also depends on files that are not `*.nix`:
sops secret files referenced through `sopsFile`, and scripts or config files
pulled in with `builtins.readFile` or a path literal. A commit that changes
only such a file changes the system but triggers no rebuild. The change stays
unapplied until an unrelated commit touches a `.nix` file. This is the worse
of the two errors: the host silently runs a configuration that differs from
the repository.

Measured on the reference deployment (three hosts, the last 25 first-parent
commits, each host's derivation evaluated per commit):

| | File hashes | Derivation |
|---|---|---|
| Rebuilds | 27 (9 commits × 3 hosts) | 11 |
| Rebuilds that changed nothing | 17 | 0 |
| Missed system changes | 2 (sops secret files only) | 0 |

One evaluation built nothing and took about 10 s on the x86 host and 24 s
with 0.8 GB peak memory on a Raspberry Pi 4 class host (4 cores, 8 GB),
run as root without the evaluation cache. Both hosts produced the identical
derivation path for the ARM system, so the result does not depend on where
it is evaluated.

## Decision

A new setting `nixos_rebuild.change_detection` selects the method:

- `files` (default): today's behavior, unchanged.
- `derivation`: rebuild if and only if the derivation path of the host's
  system differs from the one recorded at the last applied rebuild.

In `derivation` mode the phase evaluates

```
nix eval --raw <ref>#nixosConfigurations.<name> --apply \
  'c: (c.extendModules { modules = [ { system.configurationRevision = c.pkgs.lib.mkForce null; } ]; }).config.system.build.toplevel.drvPath'
```

where `<ref>#<name>` is the configured `flake`.

- **The revision is neutralized for the comparison only.** Without that, the
  derivation changes on every commit and nothing is gained. The rebuild
  itself still uses the real configuration, so a rebuilt host records the
  commit it was built from. A host the commit did not affect keeps the
  revision of its last real change, which is accurate rather than stale.
- **One evaluation per commit.** With locked inputs and pure evaluation, the
  same commit always gives the same derivation path. skipper keeps the last
  `(commit, drvPath)` in memory and evaluates again only when HEAD moved. A
  restart costs one extra evaluation.
- **State.** The derivation path is stored in the `_nixos` entry next to the
  file hashes, under a reserved key that cannot collide with a file path.
  Pre-saving before the rebuild, reverting on a surviving failure and the
  in-flight marker apply to the whole entry, so ADR-0005, ADR-0015 and
  ADR-0025 work without change. The file hashes are kept only to list the
  changed files in the event. They no longer decide anything.
- **An evaluation failure is a phase failure**, like a hashing failure today:
  stack deploys abort, a `_nixos` failed event is emitted, nothing is
  persisted. "Could not evaluate" must never read as "unchanged".
- **A missing derivation key counts as changed.** Switching from `files` to
  `derivation`, or a fresh state, rebuilds once.

## Alternatives considered

- **Per-host path filters** (include/exclude globs per host). They repeat the
  module import graph by hand. A shared module missing from a filter is a
  missed rebuild, the silent kind of error. Secrets still need listing.
  Rejected.
- **Hash a wider file set** (every file outside the stack directories). Fixes
  the misses, makes the extra rebuilds worse. Rejected.
- **Compare against `/run/current-system`.** The running system's path
  contains the revision, so it differs from HEAD after every commit. It would
  also turn skipper into a drift corrector that undoes a manual
  `nixos-rebuild test`, a behavior change that needs its own decision.
  Rejected.
- **Evaluate without neutralizing the revision.** Every commit changes the
  derivation. No gain. Rejected.
- **Evaluate on every reconcile tick.** Costs an evaluation per tick for an
  answer that only changes with HEAD. Rejected in favor of the per-commit
  cache.
- **Make `derivation` the default.** It needs a flake with a
  `nixosConfigurations` attribute and costs an evaluation per commit. Existing
  setups keep their behavior until they opt in.

## Consequences

- A host rebuilds when its own system changes, including changes that come
  only from secret files or embedded non-nix files.
- Each new commit costs one evaluation per host: about 10 s on x86, 24 s and
  0.8 GB on a Raspberry Pi 4 class host. A host with less than about 1 GB of
  free memory is not a good fit for this mode.
- Only `system.configurationRevision` is neutralized. A configuration that
  puts the revision somewhere else (`self.rev`, `self.lastModified`, the
  flake source copied into the store) changes on every commit. The mode then
  falls back to rebuilding on every commit, never to missing a rebuild. The
  user docs say so.
- A configuration that uses import-from-derivation builds during evaluation.
  skipper does not forbid it.
- When only a non-nix input changed, the event's changed-file list is empty.
  The rebuild is still correct; the log line says what happened.
- Drift of the running system (a manual switch from another checkout) is not
  corrected, as before.
