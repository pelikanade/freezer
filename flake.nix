{
  description = "Webhook-driven Nix builds and signed S3 binary-cache publication";

  inputs.nixpkgs.url = "github:NixOS/nixpkgs/c7def046b9a883d46974757852106483d741586f";

  outputs = { self, nixpkgs }:
    let
      systems = [ "x86_64-linux" "aarch64-linux" ];
      forSystems = nixpkgs.lib.genAttrs systems;
    in {
      packages = forSystems (system:
        let pkgs = import nixpkgs { inherit system; };
        in rec {
          freezer = pkgs.callPackage ./nix/package.nix { };
          default = freezer;
        });

      checks = forSystems (system: { package = self.packages.${system}.freezer; });
    };
}
