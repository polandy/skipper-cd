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
          default = pkgs.buildGoModule {
            pname = "skipper-cd";
            version = "0.1.0";
            src = ./.;
            vendorHash = null;
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
