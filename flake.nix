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
            src = ./.;
            vendorHash = null;
            # Surface the deployed version in the UI header and /api/version.
            ldflags = [ "-X main.version=${version}" ];
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
