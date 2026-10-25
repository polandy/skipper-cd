# Eval-only check of module.nix: evaluates the module with and without Docker
# and fails the build if the generated skipper-cd unit does not match. No VM —
# only the evaluated systemd unit is inspected.
{ nixpkgs, system, module, package }:

let
  inherit (nixpkgs) lib;
  pkgs = nixpkgs.legacyPackages.${system};

  unitWith = dockerEnable: (lib.nixosSystem {
    inherit system;
    modules = [
      module
      {
        # A container config skips the bootloader and file-system assertions.
        boot.isContainer = true;
        system.stateVersion = lib.trivial.release;
        services.skipper-cd = {
          enable = true;
          inherit package;
          configFile = "/etc/skipper.yml";
          docker.enable = dockerEnable;
        };
      }
    ];
  }).config.systemd.services.skipper-cd;

  hasPkg = name: unit: lib.any (p: lib.getName p == name) unit.path;

  facts = unit: {
    requiresDocker = lib.elem "docker.service" unit.requires;
    afterDocker = lib.elem "docker.service" unit.after;
    dockerGroup = lib.elem "docker" unit.serviceConfig.SupplementaryGroups;
    dockerCli = hasPkg "docker" unit;
    # Positive control: unaffected by the option either way.
    afterNetwork = lib.elem "network-online.target" unit.after;
  };

  expected = {
    withDocker = { requiresDocker = true; afterDocker = true; dockerGroup = true; dockerCli = true; afterNetwork = true; };
    withoutDocker = { requiresDocker = false; afterDocker = false; dockerGroup = false; dockerCli = false; afterNetwork = true; };
  };

  actual = {
    withDocker = facts (unitWith true);
    withoutDocker = facts (unitWith false);
  };

  mismatches = lib.concatMap
    (case: lib.mapAttrsToList
      (fact: want: "${case}.${fact}: expected ${lib.boolToString want}, got ${lib.boolToString actual.${case}.${fact}}")
      (lib.filterAttrs (fact: want: actual.${case}.${fact} != want) expected.${case}))
    (lib.attrNames expected);
in
pkgs.runCommand "skipper-cd-module-check" { } (
  if mismatches == [ ] then "touch $out"
  else ''
    echo "module.nix check failed:" >&2
    ${lib.concatMapStringsSep "\n" (m: "echo ${lib.escapeShellArg "  ${m}"} >&2") mismatches}
    exit 1
  ''
)
