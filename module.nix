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

    nixosRebuild = lib.mkOption {
      type = lib.types.bool;
      default = false;
      description = "Whether nixos_rebuild is configured in skipper.yml. Relaxes sandboxing so nixos-rebuild can run.";
    };
  };

  config = lib.mkIf cfg.enable {
    systemd.services.skipper-cd = {
      description = "skipper-cd Docker Compose CD";
      wantedBy = [ "multi-user.target" ];
      after = [ "docker.service" "network-online.target" ];
      requires = [ "docker.service" ];

      path = [ pkgs.git pkgs.docker pkgs.docker-compose pkgs.docker-buildx ]
        ++ lib.optionals cfg.nixosRebuild [
          pkgs.nixos-rebuild
          pkgs.nix
          pkgs.systemd
        ];

      serviceConfig = {
        ExecStart = "${cfg.package}/bin/skipper -config ${cfg.configFile}";
        Restart = "on-failure";
        RestartSec = "5s";
        StateDirectory = "skipper";

        # Use the state directory as HOME so docker compose can write
        # CLI config (e.g. buildx state) under ProtectSystem=strict.
        Environment = "HOME=${cfg.stateDir}";

        # Allow Docker socket access
        SupplementaryGroups = [ "docker" ];

        # Hardening
        NoNewPrivileges = !cfg.nixosRebuild;
        PrivateTmp = true;
        ProtectSystem = if cfg.nixosRebuild then "false" else "strict";
        ProtectHome = if cfg.nixosRebuild then "read-only" else true;
        ReadWritePaths = [ cfg.stateDir ]
          ++ lib.optionals cfg.nixosRebuild [ "/nix" "/run" "/etc/NIXOS" ];
        ReadOnlyPaths = [ "/run/secrets" ];
      };
    };
  };
}
