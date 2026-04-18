{ config, lib, pkgs, ... }:

let
  cfg = config.services.skipper-cd;
in
{
  options.services.skipper-cd = {
    enable = lib.mkEnableOption "skipper-cd Docker Compose CD";

    package = lib.mkOption {
      type = lib.types.package;
      description = "The skipper-cd package to use.";
    };

    configFile = lib.mkOption {
      type = lib.types.path;
      description = "Path to the skipper.yml config file.";
    };

    stateDir = lib.mkOption {
      type = lib.types.str;
      default = "/var/lib/skipper";
      description = "Directory for storing deploy state.";
    };
  };

  config = lib.mkIf cfg.enable {
    systemd.services.skipper-cd = {
      description = "skipper-cd Docker Compose CD";
      wantedBy = [ "multi-user.target" ];
      after = [ "docker.service" "network-online.target" ];
      requires = [ "docker.service" ];

      serviceConfig = {
        ExecStart = "${cfg.package}/bin/skipper -config ${cfg.configFile}";
        Restart = "on-failure";
        RestartSec = "5s";
        StateDirectory = "skipper";

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
