# VM test of the strict (non-nixosRebuild) sandbox: skipper-cd must still clone
# a repo and bring a stack up through Docker, and its systemd exposure score
# must stay at or below the threshold.
{ nixpkgs, system, module, package }:

let
  pkgs = nixpkgs.legacyPackages.${system};

  # On the 0-100 scale of --threshold (exposure x10).
  maxExposure = "45";

  image = pkgs.dockerTools.buildImage {
    name = "sandbox-probe";
    tag = "1";
    copyToRoot = [ pkgs.busybox ];
    config.Cmd = [ "sleep" "infinity" ];
  };

  skipperConfig = pkgs.writeText "skipper.yml" ''
    repo_url: file:///srv/deploy.git
    stacks_base_dir: stacks
  '';

  compose = pkgs.writeText "docker-compose.yml" ''
    services:
      probe:
        image: sandbox-probe:1
        pull_policy: never
  '';
in
pkgs.testers.runNixOSTest {
  name = "skipper-cd-sandbox";

  nodes.machine = { lib, ... }: {
    imports = [ module ];
    virtualisation.docker.enable = true;
    environment.systemPackages = [ pkgs.git ];
    services.skipper-cd = {
      enable = true;
      inherit package;
      configFile = skipperConfig;
    };
    # Started by the test once the repo and image exist.
    systemd.services.skipper-cd.wantedBy = lib.mkForce [ ];
  };

  testScript = ''
    machine.wait_for_unit("docker.service")
    machine.succeed("docker load < ${image}")
    machine.succeed(
      "git init -q -b main /srv/work"
      " && mkdir -p /srv/work/stacks/probe"
      " && cp ${compose} /srv/work/stacks/probe/docker-compose.yml"
      " && git -C /srv/work add -A"
      " && git -C /srv/work -c user.name=t -c user.email=t@t commit -qm init"
      " && git clone -q --bare /srv/work /srv/deploy.git"
    )

    machine.succeed("systemctl start skipper-cd.service")
    machine.wait_until_succeeds("docker ps --filter status=running --format '{{.Image}}' | grep -qx sandbox-probe:1")
    machine.succeed("test -f /var/lib/skipper/state.yaml")
    machine.succeed("systemctl is-active skipper-cd.service")

    print(machine.succeed("systemd-analyze security --no-pager skipper-cd.service | tail -1"))
    machine.succeed("systemd-analyze security --threshold=${maxExposure} skipper-cd.service")
  '';
}
