{
  description = "skipper-cd — lightweight Docker Compose CD for NixOS";

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

      devShells = forAllSystems (system:
        let pkgs = nixpkgs.legacyPackages.${system}; in
        {
          default = pkgs.mkShell {
            buildInputs = [ pkgs.go pkgs.gopls pkgs.gotools ];
          };
        });
    };
}
