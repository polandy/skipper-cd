{ config, lib, pkgs, ... }:

let
  cfg = config.services.orpheus-cd;
in
{
  options.services.orpheus-cd = {
    enable = lib.mkEnableOption "orpheus-cd Docker Compose CD";

    package = lib.mkOption {
      type = lib.types.package;
      description = "The orpheus-cd package to use.";
    };

    configFile = lib.mkOption {
      type = lib.types.path;
      description = "Path to the orpheus.yml config file.";
    };

    stateDir = lib.mkOption {
      type = lib.types.str;
      default = "/var/lib/orpheus";
      description = "Directory for storing deploy state.";
    };
  };

  config = lib.mkIf cfg.enable {
    systemd.services.orpheus-cd = {
      description = "orpheus-cd Docker Compose CD";
      wantedBy = [ "multi-user.target" ];
      after = [ "docker.service" "network-online.target" ];
      requires = [ "docker.service" ];

      serviceConfig = {
        ExecStart = "${cfg.package}/bin/orpheus -config ${cfg.configFile}";
        Restart = "on-failure";
        RestartSec = "5s";
        StateDirectory = "orpheus";

        # Allow Docker socket access
        SupplementaryGroups = [ "docker" ];

        # Hardening
        NoNewPrivileges = true;
        PrivateTmp = true;
        ProtectSystem = "strict";
        ProtectHome = true;
        ReadWritePaths = [ cfg.stateDir ];
        ReadOnlyPaths = [ "/run/secrets" "/etc/nixos" ];
      };
    };
  };
}
