{
  description = "Polling Nix flake build service with signed S3 binary-cache publication";

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

      nixosModules.default = import ./nix/module.nix { inherit self; };

      checks = forSystems (system:
        let
          pkgs = import nixpkgs { inherit system; };
          node = nixpkgs.lib.nixosSystem {
            inherit system;
            modules = [
              self.nixosModules.default
              {
                services.freezer = {
                  enable = true;
                  environmentFile = "/run/secrets/freezer-environment";
                  cache = {
                    bucket = "smoke";
                    endpoint = "https://example.invalid";
                  };
                  repositories = [
                    {
                      name = "smoke";
                      url = "https://example.invalid/smoke.git";
                      github = "smoke/smoke";
                    }
                  ];
                };
                system.stateVersion = "26.05";
              }
            ];
          };
        in {
          package = self.packages.${system}.freezer;
          # Evaluate the module for this system and build the unit it generates.
          nixos-module = pkgs.writeText "freezer.service" node.config.systemd.units."freezer.service".text;
        });
    };
}
