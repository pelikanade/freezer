{ self }:
{ config, lib, pkgs, utils, ... }:

let
  cfg = config.services.freezer;
in {
  options.services.freezer = {
    enable = lib.mkEnableOption "Freezer Nix build service";
    package = lib.mkOption {
      type = lib.types.package;
      default = self.packages.${pkgs.stdenv.hostPlatform.system}.default;
      defaultText = lib.literalExpression "inputs.freezer.packages.<system>.default";
      description = "Freezer executable. The host supplies Nix >= 2.33 (2.34 tested).";
    };
    configFile = lib.mkOption {
      type = lib.types.str;
      example = "/run/secrets/freezer-config.yaml";
      description = ''
        Absolute path to the private server YAML, readable by freezer.
        Use a string, not a Nix path, so webhook secrets never enter the store.
        Set state_dir to /var/lib/freezer and signing_key to a 0600 file
        owned by freezer outside the Nix store.
      '';
    };
    environmentFile = lib.mkOption {
      type = lib.types.nullOr lib.types.str;
      default = null;
      example = "/run/secrets/freezer-environment";
      description = ''
        Absolute path to a systemd environment file containing GITHUB_TOKEN,
        AWS_ACCESS_KEY_ID and AWS_SECRET_ACCESS_KEY. Keep its contents out of
        Nix expressions and restrict access to root.
      '';
    };
  };

  config = lib.mkIf cfg.enable {
    assertions = [
      {
        assertion = lib.hasPrefix "/" cfg.configFile;
        message = "services.freezer.configFile must be an absolute runtime path.";
      }
      {
        assertion = cfg.environmentFile == null || lib.hasPrefix "/" cfg.environmentFile;
        message = "services.freezer.environmentFile must be an absolute runtime path.";
      }
    ];

    users.groups.freezer = { };
    users.users.freezer = {
      isSystemUser = true;
      group = "freezer";
      home = "/var/lib/freezer";
    };

    systemd.services.freezer = {
      description = "Freezer Nix build service";
      wantedBy = [ "multi-user.target" ];
      wants = [ "network-online.target" ];
      after = [ "network-online.target" "nix-daemon.socket" ];
      path = [ pkgs.git config.nix.package ];
      environment = {
        HOME = "/var/lib/freezer";
        XDG_CACHE_HOME = "/var/lib/freezer/cache";
        GIT_TERMINAL_PROMPT = "0";
        AWS_EC2_METADATA_DISABLED = "true";
      };
      serviceConfig = {
        Type = "simple";
        User = "freezer";
        Group = "freezer";
        StateDirectory = "freezer";
        StateDirectoryMode = "0700";
        WorkingDirectory = "/var/lib/freezer";
        ExecStart = utils.escapeSystemdExecArgs [ (lib.getExe cfg.package) cfg.configFile ];
        Restart = "on-failure";
        RestartSec = 5;
        TimeoutStopSec = 10;
        UMask = "0077";
        NoNewPrivileges = true;
        PrivateTmp = true;
        ProtectSystem = "strict";
        ProtectHome = true;
      } // lib.optionalAttrs (cfg.environmentFile != null) {
        EnvironmentFile = cfg.environmentFile;
      };
    };
  };
}
