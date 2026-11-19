# Eval-only check of module.nix: evaluates the module across its options and
# fails the build if the generated skipper-cd unit does not match. No VM —
# only the evaluated systemd unit is inspected.
{ nixpkgs, system, module, package }:

let
  inherit (nixpkgs) lib;
  pkgs = nixpkgs.legacyPackages.${system};

  hostWith = opts: (lib.nixosSystem {
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
        } // opts;
      }
    ];
  }).config;

  unitWith = opts: (hostWith opts).systemd.services.skipper-cd;

  hasPkg = name: unit: lib.any (p: lib.getName p == name) unit.path;

  dockerFacts = unit: {
    requiresDocker = lib.elem "docker.service" unit.requires;
    afterDocker = lib.elem "docker.service" unit.after;
    dockerGroup = lib.elem "docker" unit.serviceConfig.SupplementaryGroups;
    dockerCli = hasPkg "docker" unit;
    # Positive control: unaffected by the option either way.
    afterNetwork = lib.elem "network-online.target" unit.after;
  };

  sc = unit: unit.serviceConfig;
  isOn = unit: key: (sc unit).${key} or false == true;

  hardeningFacts = unit: {
    protectKernel = isOn unit "ProtectKernelTunables" && isOn unit "ProtectKernelModules" && isOn unit "ProtectKernelLogs";
    protectControlGroups = isOn unit "ProtectControlGroups";
    privateDevices = isOn unit "PrivateDevices";
    restrictNamespaces = isOn unit "RestrictNamespaces";
    restrictSUIDSGID = isOn unit "RestrictSUIDSGID";
    lockPersonality = isOn unit "LockPersonality";
    syscallFilter = (sc unit) ? SystemCallFilter;
    addressFamilies = (sc unit) ? RestrictAddressFamilies;
    # Positive controls: set in both modes.
    privateTmp = isOn unit "PrivateTmp";
    protectSystemStrict = (sc unit).ProtectSystem == "strict";
  };

  stateDirFacts = opts:
    let
      host = hostWith opts;
      unit = host.systemd.services.skipper-cd;
      dir = opts.stateDir;
    in
    {
      stateDirectoryMatches = ((sc unit).StateDirectory or null) == lib.removePrefix "/var/lib/" dir;
      writable = lib.elem dir (sc unit).ReadWritePaths;
      home = (sc unit).Environment == "HOME=${dir}";
      createdByTmpfiles = lib.any (r: lib.hasPrefix "d ${dir} " r) host.systemd.tmpfiles.rules;
    };

  allOn = facts: lib.mapAttrs (_: _: true) facts;

  expected = {
    withDocker = { requiresDocker = true; afterDocker = true; dockerGroup = true; dockerCli = true; afterNetwork = true; };
    withoutDocker = { requiresDocker = false; afterDocker = false; dockerGroup = false; dockerCli = false; afterNetwork = true; };
    hardenedWithoutRebuild = allOn (hardeningFacts (unitWith { }));
    # nix evaluates as root against the local store, which needs namespaces and a writable /nix.
    relaxedForRebuild = {
      protectKernel = false; protectControlGroups = false; privateDevices = false; restrictNamespaces = false;
      restrictSUIDSGID = false; lockPersonality = false; syscallFilter = false; addressFamilies = false;
      privateTmp = true; protectSystemStrict = false;
    };
    defaultStateDir = { stateDirectoryMatches = true; writable = true; home = true; createdByTmpfiles = false; };
    stateDirUnderVarLib = { stateDirectoryMatches = true; writable = true; home = true; createdByTmpfiles = false; };
    stateDirElsewhere = { stateDirectoryMatches = false; writable = true; home = true; createdByTmpfiles = true; };
  };

  actual = {
    withDocker = dockerFacts (unitWith { docker.enable = true; });
    withoutDocker = dockerFacts (unitWith { docker.enable = false; });
    hardenedWithoutRebuild = hardeningFacts (unitWith { });
    relaxedForRebuild = hardeningFacts (unitWith { nixosRebuild = true; });
    defaultStateDir = stateDirFacts { stateDir = "/var/lib/skipper"; };
    stateDirUnderVarLib = stateDirFacts { stateDir = "/var/lib/cd/skipper"; };
    stateDirElsewhere = stateDirFacts { stateDir = "/srv/skipper"; };
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
