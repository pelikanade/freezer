{ lib, buildGoModule }:

let
  rootFiles = builtins.readDir ../.;
  goSources = map (name: ../. + "/${name}")
    (builtins.filter (name:
      rootFiles.${name} == "regular" && lib.hasSuffix ".go" name && !(lib.hasSuffix "_test.go" name)
    ) (builtins.attrNames rootFiles));
in
buildGoModule {
  pname = "freezer";
  version = "0.2.3";

  src = lib.fileset.toSource {
    root = ../.;
    fileset = lib.fileset.unions (goSources ++ [ ../go.mod ../go.sum ../web ]);
  };

  vendorHash = "sha256-ym2IdO2nmzfaIVErrPSQYlcTNAFKj+/oQnPBAPsxbaE=";
  subPackages = [ "." ];
  env.CGO_ENABLED = "0";
  ldflags = [ "-s" "-w" ];
  # The acceptance suite needs the host Nix daemon and the devenv S3 service.
  doCheck = false;

  meta = {
    description = "Polling Nix flake build service with a signed S3 binary cache";
    mainProgram = "freezer";
    platforms = [ "x86_64-linux" "aarch64-linux" ];
  };
}
