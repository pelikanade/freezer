{ pkgs, lib, config, inputs, ... }:

{
  # https://devenv.sh/languages/
  languages.go = {
    enable = true;

    # Type your own Go version if needed. This pulls the `go-overlay` input
    # (added to devenv.yaml/devenv.lock on first evaluation) and builds a
    # toolchain that may have no binary cache hit.
    # version = "1.24.0";
  };

  # https://devenv.sh/packages/
  packages = [ pkgs.git ];

  # https://devenv.sh/scripts/
  scripts.init.exec = ''
    go mod init "$(basename "$PWD")"
  '';

  # https://devenv.sh/tests/
  enterTest = ''
    go version
  '';
}
