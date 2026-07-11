{ config, lib, pkgs, ... }:

let
  cfg = config.services.skipper-cd;
  systemctl = "${config.systemd.package}/bin/systemctl";

  # Restart the service only when it has entered a *failed* state — e.g. after a
  # self-update whose stop was force-killed at TimeoutStopSec (see ADR-0014,
  # ADR-0017). systemd's Restart= directive deliberately does not act on a
  # commanded or aborted stop, so without this a wedged self-update would leave
  # the host without CD until a manual `systemctl reset-failed && start`. A plain
  # intentional stop leaves the unit "inactive" (not "failed"), so it is left
  # alone. is-failed exits 0 when failed; the `if` keeps this oneshot green.
  recoverScript = pkgs.writeShellScript "skipper-cd-recover" ''
    if ${systemctl} is-failed --quiet skipper-cd.service; then
      echo "skipper-cd.service is failed; restarting"
      ${systemctl} restart skipper-cd.service
    fi
  '';
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

    stopTimeout = lib.mkOption {
      type = lib.types.str;
      default = "15min";
      description = ''
        systemd TimeoutStopSec for the service. On shutdown skipper-cd
        waits for an in-flight deploy to finish (see ADR-0007); this must
        be longer than a typical deploy run or systemd will SIGKILL the
        service mid-deploy after the default 90 seconds.
      '';
    };

    autoRecover = lib.mkOption {
      type = lib.types.bool;
      default = true;
      description = ''
        Install a timer that restarts skipper-cd whenever it is in a failed
        state. This self-heals a self-update whose stop was force-killed
        (ADR-0014, ADR-0017), which systemd's Restart= directive cannot cover
        because it does not act on a commanded stop. An intentional
        `systemctl stop` leaves the unit inactive (not failed) and is untouched.
      '';
    };

    recoverInterval = lib.mkOption {
      type = lib.types.str;
      default = "2min";
      description = ''
        How often the autoRecover timer checks for and clears a failed
        skipper-cd service (systemd time span, e.g. "2min"). Ignored when
        autoRecover is false.
      '';
    };
  };

  config = lib.mkIf cfg.enable {
    systemd.services.skipper-cd = {
      description = "skipper-cd Docker Compose CD";
      wantedBy = [ "multi-user.target" ];
      # After= alone does not pull network-online.target into the boot
      # transaction; Wants= is required for the ordering to take effect.
      after = [ "docker.service" "network-online.target" ];
      wants = [ "network-online.target" ];
      requires = [ "docker.service" ];

      path = [ pkgs.git pkgs.docker pkgs.docker-compose pkgs.docker-buildx ]
        ++ lib.optionals cfg.nixosRebuild [
          pkgs.nixos-rebuild
          pkgs.nix
          pkgs.systemd
        ];

      serviceConfig = {
        ExecStart = "${cfg.package}/bin/skipper -config ${cfg.configFile}";
        # "always" keeps the CD service up across any spontaneous exit; a
        # failed/killed self-update stop is caught by the autoRecover timer
        # instead, which Restart= cannot cover (ADR-0017).
        Restart = "always";
        RestartSec = "5s";
        TimeoutStopSec = cfg.stopTimeout;
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
        # "-" prefix: ignore when absent — /run/secrets only exists on
        # hosts using sops-nix; a hard reference fails namespace setup
        # (status=226/NAMESPACE) everywhere else.
        ReadOnlyPaths = [ "-/run/secrets" ];
      };
    };

    # Self-heal a failed self-update (ADR-0017): a oneshot that restarts the
    # service only when it is in a failed state, driven by a periodic timer.
    systemd.services.skipper-cd-recover = lib.mkIf cfg.autoRecover {
      description = "Restart skipper-cd if it has entered a failed state";
      serviceConfig = {
        Type = "oneshot";
        ExecStart = recoverScript;
      };
    };

    systemd.timers.skipper-cd-recover = lib.mkIf cfg.autoRecover {
      description = "Periodic recovery check for a failed skipper-cd service";
      wantedBy = [ "timers.target" ];
      timerConfig = {
        OnBootSec = cfg.recoverInterval;
        OnUnitActiveSec = cfg.recoverInterval;
      };
    };
  };
}
