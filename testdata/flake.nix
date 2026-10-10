{
  inputs.busybox = { url = "path:@busyboxRoot@"; flake = false; };
  outputs = { self, busybox }: let
    system = "@system@";
    tool = "${busybox}/bin/busybox";
    make = name: script: builtins.derivation {
      inherit name system tool;
      builder = tool;
      args = [ "sh" "-eu" "-c" script ];
    };
    basic = name: make name ''
      "$tool" mkdir -p "$out"
      printf 'freezer fixture: %s\n' '${name}' > "$out/message"
    '';
    generated = make "freezer-expression" ''
      printf '"IFD evaluated"' > "$out"
    '';
  in {
    packages.${system} = {
      hello = basic "freezer-hello";
      second = basic "freezer-second";
      multi = builtins.derivation {
        name = "freezer-multi";
        inherit system tool;
        builder = tool;
        outputs = [ "out" "dev" ];
        args = [ "sh" "-eu" "-c" ''
          "$tool" mkdir -p "$out" "$dev"
          printf 'default output\n' > "$out/message"
          printf 'development output\n' > "$dev/message"
        '' ];
      };
      ifd = make "freezer-ifd" ''
        "$tool" mkdir -p "$out"
        printf '%s\n' '${import generated}' > "$out/message"
      '';
      slow = make "freezer-slow-@nonce@" ''
        echo 'slow fixture started'
        "$tool" sleep 30
        "$tool" mkdir -p "$out"
        printf 'finished\n' > "$out/message"
      '';
      broken = make "freezer-broken" ''
        echo 'error: fixture cannot build' >&2
        exit 1
      '';
      notDerivation = 42;
    };
    selected.${system} = {
      alpha = self.packages.${system}.hello;
      beta = self.packages.${system}.second;
    };
  };
}
