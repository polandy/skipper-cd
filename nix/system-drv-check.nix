# Eval-only check of the expression derivation change detection applies to a
# nixosConfigurations entry (ADR-0065): a new commit alone
# (system.configurationRevision) must not change the system derivation, while a
# real option change must. Nothing is built — only drvPaths are compared.
{ nixpkgs, system }:

let
  inherit (nixpkgs) lib;
  pkgs = nixpkgs.legacyPackages.${system};

  # Verbatim copy of nixos.SystemDrvApplyExpr; a Go test keeps the two equal.
  systemDrv = c: (c.extendModules { modules = [ { system.configurationRevision = c.pkgs.lib.mkForce null; } ]; }).config.system.build.toplevel.drvPath;

  host = { revision, hostName ? "fixture" }: lib.nixosSystem {
    inherit system;
    modules = [
      {
        # A container config skips the bootloader and file-system assertions.
        boot.isContainer = true;
        system.stateVersion = lib.trivial.release;
        system.configurationRevision = revision;
        networking.hostName = hostName;
      }
    ];
  };

  rawDrv = c: c.config.system.build.toplevel.drvPath;

  a = host { revision = "1111111111111111111111111111111111111111"; };
  b = host { revision = "2222222222222222222222222222222222222222"; };
  renamed = host { revision = "1111111111111111111111111111111111111111"; hostName = "renamed"; };

  failures =
    # Positive control: without the neutralization the revision does reach the
    # derivation, so the equality below is not vacuous.
    lib.optional (rawDrv a == rawDrv b) "the revision does not change the raw system derivation; the check proves nothing"
    ++ lib.optional (systemDrv a != systemDrv b) "two revisions of the same configuration give different system derivations"
    ++ lib.optional (systemDrv a == systemDrv renamed) "a changed option leaves the system derivation unchanged";
in
pkgs.runCommand "skipper-cd-system-drv-check" { } (
  if failures == [ ] then "touch $out"
  else ''
    echo "system derivation check failed:" >&2
    ${lib.concatMapStringsSep "\n" (m: "echo ${lib.escapeShellArg "  ${m}"} >&2") failures}
    exit 1
  ''
)
