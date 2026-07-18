{
  description = "skipper-cd — lightweight GitOps CD for Docker Compose, with first-class NixOS support";

  inputs = {
    nixpkgs.url = "github:NixOS/nixpkgs/nixos-unstable";
  };

  outputs = { self, nixpkgs }:
    let
      systems = [ "x86_64-linux" "aarch64-linux" ];
      forAllSystems = nixpkgs.lib.genAttrs systems;
    in
    {
      packages = forAllSystems (system:
        let pkgs = nixpkgs.legacyPackages.${system}; in
        {
          default = pkgs.buildGoModule rec {
            pname = "skipper-cd";
            # Single source of truth, kept current by release-please.
            version = (builtins.fromJSON (builtins.readFile ./.release-please-manifest.json))."." ;
            # The flake knows the built commit (self.shortRev) but not the branch
            # name — so a Nix homelab build surfaces "vX.Y.Z · <sha>", identifying
            # the exact revision without a branch label.
            commit = self.shortRev or self.dirtyShortRev or "unknown";
            src = ./.;
            vendorHash = null;
            # Surface the deployed build identity in the UI header (/api/version).
            ldflags = [ "-X main.version=${version}" "-X main.commit=${commit}" ];
            meta = {
              description = "Lightweight Docker Compose CD triggered by Git webhooks";
              homepage = "https://github.com/polandy/skipper-cd";
              license = nixpkgs.lib.licenses.mit;
              mainProgram = "skipper";
            };
          };
        });

      nixosModules.default = import ./module.nix;

      # Dev shell carrying the full CI toolchain so the whole pipeline
      # (.github/workflows/ci.yml + docs.yml) can be run locally — handy on
      # NixOS where the tools aren't otherwise on PATH. Versions are checked
      # against what CI pins; see `make ci` for the local mirror of the jobs.
      devShells = forAllSystems (system:
        let
          pkgs = nixpkgs.legacyPackages.${system};
          # mkdocs + Material theme for `mkdocs build --strict`. nixpkgs tracks
          # a slightly newer Material (9.7.x) than docs/requirements.txt pins
          # (9.6.14); close enough for a local pre-check, CI stays the source of
          # truth for the published site.
          mkdocs = pkgs.python3.withPackages (ps: [ ps.mkdocs-material ]);
        in
        {
          default = pkgs.mkShell {
            buildInputs = [
              # Go pinned to go.mod's minor (what CI's setup-go installs), so
              # `go test`/govulncheck see the same stdlib as the pipeline — a
              # newer toolchain flags stdlib CVEs CI doesn't. The nix *build*
              # (packages.default) tracks pkgs.go separately. This nixpkgs pin
              # only has go_1_26 at 1.26.4, one patch behind go.mod's 1.26.5
              # (GO-2026-5856 fix) — closing that gap needs a nixpkgs bump.
              pkgs.go_1_26
              pkgs.gopls
              pkgs.gotools
              pkgs.gcc # cgo — required by `go test -race`
              pkgs.git # real git for internal/git + e2e tests
              # Lint / security jobs.
              pkgs.golangci-lint # CI pins v2.12
              pkgs.govulncheck
              pkgs.trivy # image CVE scan (needs a running dockerd — see make)
              # UI E2E: Node for `npm ci` + Playwright's JS runner.
              pkgs.nodejs_22
              # Docs site.
              mkdocs
            ];
            # Playwright's browser binary from nixpkgs, patched to run on NixOS
            # and version-matched to @playwright/test 1.61.1 (e2e/ui). This
            # replaces `npx playwright install` — the JS package still comes
            # from `npm ci`, only the browser is provided here.
            PLAYWRIGHT_BROWSERS_PATH = "${pkgs.playwright-driver.browsers}";
            PLAYWRIGHT_SKIP_BROWSER_DOWNLOAD = "1";
            PLAYWRIGHT_SKIP_VALIDATE_HOST_REQUIREMENTS = "true";
          };
        });
    };
}
