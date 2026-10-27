# Feature Spec: NixOS Change Detection by System Derivation

Status: implemented (see [ADR-0065](adr/0065-nixos-change-detection-by-system-derivation.md)).
Date: 2026-10-27

## Goal

The `_nixos` phase rebuilds a host when that host's system changed, and only
then. A change to another host's files no longer rebuilds this one. A change
to a non-nix input (a sops secret file, a script pulled in with
`builtins.readFile`) no longer goes unnoticed.

Non-goals: correcting drift of the running system, per-host path filters,
changing how the rebuild itself runs (ADR-0014), any UI change.

## User model

```yaml
nixos_rebuild:
  flake: ".#myhost"
  change_detection: derivation   # files (default) | derivation
```

- `files`: rebuild when any `*.nix` file or `flake.lock` in the repository
  changed. Today's behavior, the default.
- `derivation`: rebuild when the derivation of `myhost`'s system changed.
  `system.configurationRevision` is ignored for the comparison.

## Configuration

- `NixOSRebuild` gains `ChangeDetection string` (`yaml:"change_detection"`).
  The two values are named constants; empty means `files`.
- Validation in `Load`:
  - An unknown value is rejected with an error naming the allowed values.
  - In `derivation` mode the `flake` must name an attribute (`<ref>#<name>`,
    both parts non-empty). `nixos-rebuild` falls back to the hostname when the
    attribute is missing; the evaluation does not guess, so a missing
    attribute is a load error.

## Evaluation

`internal/nixos` gains

```go
// SystemDerivation evaluates the drvPath of the flake's NixOS system with
// system.configurationRevision forced to null (ADR-0065).
func SystemDerivation(ctx context.Context, out Outputter, repoDir, flake string) (string, error)
```

It is a package-level function over an `Outputter` (the Deployer's
output-capturing runner), not a `Rebuilder` method over the `command.Runner`:
the evaluation needs stdout, which `Runner.Run` does not return. It runs with
`repoDir` as working directory and the normal per-command timeout (ADR-0008);
a failure carries the stderr tail, so the failed event names the cause
(ADR-0063):

```
nix --extra-experimental-features "nix-command flakes" eval --raw \
  <ref>#nixosConfigurations."<name>" \
  --apply 'c: (c.extendModules { modules = [ { system.configurationRevision = c.pkgs.lib.mkForce null; } ]; }).config.system.build.toplevel.drvPath'
```

- The evaluation is read-only and short, so it runs as a direct child, not in
  a transient unit. ADR-0014 is about the switch restarting skipper; an
  evaluation cannot do that.
- The output is trimmed and must be an absolute store path ending in `.drv`.
  Anything else is an error, so an empty or garbled output never reads as a
  valid derivation.
- The `--apply` expression is a named constant. The flake check
  `nix/system-drv-check.nix` evaluates the same expression against a minimal
  `nixosSystem`: two revisions give one derivation, a changed option another.
- The attribute name is quoted, as `nixos-rebuild` does, so a host name with a
  dot stays one attribute.

## State

The `_nixos` entry in `state.yaml` keeps the file hashes and gains one key:

```yaml
stacks:
  _nixos:
    /var/lib/skipper/repo/flake.lock: 3f1c…
    /var/lib/skipper/repo/system/myhost/default.nix: 9a0e…
    "@system-drv": /nix/store/6aq5…-nixos-system-myhost-26.05.drv
```

- The reserved key starts with `@`. File keys are absolute paths, so the two
  never collide. The key is a named constant in `internal/nixos`.
- `DiffHashes` callers in the `_nixos` phase skip the reserved key, so it
  never shows up as a changed file.
- `files` mode ignores the key. Switching back to `files` leaves a stale key
  behind; it is overwritten on the next `derivation`-mode rebuild and never
  read in `files` mode.

## Run algorithm (derivation mode)

Replaces the change decision in `rebuildNixOSIfChanged`. Everything after the
decision (autosync gate, pre-save, in-flight marker, rebuild, revert on a
surviving failure, shutdown handling, reconciliation) stays as it is.

1. Hash the nix files as today. A hashing failure is a phase failure, as
   today.
2. Get the derivation for HEAD. The `Deployer` keeps the last
   `(commit, drvPath)` in memory. If HEAD equals the cached commit, reuse the
   path; otherwise evaluate and replace the cache. The cache is only updated
   on success.
3. An evaluation failure is a phase failure, handled like a hashing failure:
   error logged, `skipper_deploy_errors_total{stack="_nixos"}` incremented,
   `_nixos` failed event with the condensed cause (ADR-0063), stack deploys
   abort, nothing persisted.
4. Compare with the recorded `@system-drv`. A missing key counts as changed.
5. **Unchanged:** emit `skipped` as today, and record the current file hashes
   together with the unchanged derivation. Otherwise the next real rebuild
   would list nix files that changed in earlier commits without affecting
   this host.
6. **Changed:** `changed` is the list of nix files that differ from the
   recorded hashes (may be empty). The rest of the phase runs unchanged with
   the new entry (file hashes + `@system-drv`) as the value it pre-saves and,
   on a surviving failure, reverts.
7. When `changed` is empty, the success log line says the system changed
   without a nix-file change. The in-flight marker (ADR-0025) holds the
   changed-file list today; with an empty list it would read as "no rebuild
   in flight". It records the single entry `@system-drv` instead, and the
   reconciliation shows no files for it.

The `upcoming` look-ahead keeps excluding `_nixos` (UI_SPEC), so it needs no
evaluation.

## Metrics

- `skipper_nixos_evaluation_seconds` (gauge): duration of the last
  successful evaluation, so a slow host shows up in the metrics rather than
  only in a one-off measurement.

No new counter: evaluation failures count in the existing
`skipper_deploy_errors_total{stack="_nixos"}`.

## Tests

Test-first, with the fake `recordingRunner` asserting the exact argv. Every
"did not rebuild" test asserts against the recorded calls (the eval call is
present, `systemd-run` is absent) plus the emitted `skipped` event.

- `TestNixOSPhase_Derivation_SkipsWhenDrvUnchanged` — nix files changed, same
  derivation: no rebuild, `skipped`, file hashes advanced in state.
- `TestNixOSPhase_Derivation_RebuildsWhenDrvChanged`
- `TestNixOSPhase_Derivation_RebuildsWhenOnlyNonNixInputChanged` — no file
  hash differs, the derivation does: rebuild, empty changed list, in-flight
  marker set to `@system-drv`.
- `TestNixOSPhase_Derivation_EvaluatesOncePerCommit` — two runs at the same
  HEAD: one eval call. A third run at a new HEAD evaluates again.
- `TestNixOSPhase_Derivation_EvalFailureAbortsStacks` — failed event, no
  stack deploy, state unchanged, cache unchanged.
- `TestNixOSPhase_Derivation_RejectsMalformedEvalOutput` — empty output and
  a non-`.drv` path are failures.
- `TestNixOSPhase_Derivation_RevertsDrvOnSurvivingFailure` (ADR-0015)
- `TestNixOSPhase_Derivation_KeepsDrvAndMarkerOnShutdown` (ADR-0005, ADR-0025)
- `TestNixOSPhase_Derivation_MissingDrvKeyRebuilds` — state written in
  `files` mode.
- `TestNixOSPhase_Files_NeverEvaluates` — default mode: no eval call.
- `TestDiffHashes_IgnoresReservedDrvKey`
- Config: `change_detection` unknown value rejected; `derivation` without
  `#name` rejected; empty defaults to `files`.

The `--apply` expression is the one part a fake runner cannot check. A flake
check in `nix/` evaluates a minimal `nixosSystem` fixture twice, with two
different `configurationRevision` values, and asserts the same derivation
path, and a third time with a changed option, asserting a different one.
Evaluation only, nothing is built.

## Docs

- `docs/nixos.md`: `change_detection` row in the `nixos_rebuild` table, a
  short paragraph on what `derivation` compares, the evaluation cost
  (measured: ~10 s on x86; 24 s and 0.8 GB peak memory on a Raspberry Pi 4
  class host), and the
  caveat that revision values other than `configurationRevision` make every
  commit a change.
- `docs/configuration.md`: the `nixos_rebuild` example gains the field.
- `docs/state.md`: the `@system-drv` key under `_nixos`.
- `docs/metrics.md`: the new gauge.
- `CLAUDE.md` invariant 4: the change decision depends on the mode; an
  evaluation failure is a phase failure like a hashing failure.
- `dev-docs/adr/README.md` and `dev-docs/README.md`: index entries.

## Open questions

- Whether the success event should carry a short note when the changed-file
  list is empty. Success events show no message in the UI today, so this
  would be a UI change; left out until it proves confusing.
