{ self }:
{ config, lib, pkgs, utils, ... }:

let
  cfg = config.services.freezer;

  stateDir = "/var/lib/freezer";

  settings = {
    listen = cfg.listen;
    state_dir = stateDir;
    global_build_cap = cfg.globalBuildCap;
    glob_cap = cfg.globCap;
    job_timeout = cfg.jobTimeout;
    retention = cfg.retention;
    poll_interval = cfg.pollInterval;
    github_api_base = cfg.githubApiBase;
    cache = {
      bucket = cfg.cache.bucket;
      endpoint = cfg.cache.endpoint;
      signing_key = cfg.cache.signingKey;
    };
    repositories = map (repo: { inherit (repo) name url github; }) cfg.repositories;
  } // lib.optionalAttrs (cfg.publicUrl != null) {
    public_url = cfg.publicUrl;
  };

  configFile = (pkgs.formats.yaml { }).generate "freezer.yaml" settings;
in
{
  options.services.freezer = {
    enable = lib.mkEnableOption "Freezer, a Nix flake build service publishing to a signed S3 binary cache";

    package = lib.mkOption {
      type = lib.types.package;
      default = self.packages.${pkgs.stdenv.hostPlatform.system}.default;
      defaultText = lib.literalExpression "freezer.packages.<system>.default";
      description = ''
        Freezer executable. It shells out to git and the host's Nix, which do
        every build, probe and copy.
      '';
    };

    environmentFile = lib.mkOption {
      type = lib.types.str;
      example = "/run/secrets/freezer-environment";
      description = ''
        Absolute path to a systemd environment file holding `GITHUB_TOKEN`,
        `AWS_ACCESS_KEY_ID` and `AWS_SECRET_ACCESS_KEY`. systemd reads it as
        root before dropping to the `freezer` user, so it can be root-owned
        with mode 0600.
      '';
    };

    listen = lib.mkOption {
      type = lib.types.str;
      default = "127.0.0.1:8080";
      description = "Address the web UI listens on. Keep it on loopback behind a reverse proxy or SSH tunnel.";
    };

    pollInterval = lib.mkOption {
      type = lib.types.str;
      default = "15m";
      example = "10m";
      description = "Go duration between polls of every registered repository's default-branch head. Must be positive.";
    };

    jobTimeout = lib.mkOption {
      type = lib.types.str;
      default = "1h";
      description = "Go duration allowed per job, including evaluation, probing and publication.";
    };

    globalBuildCap = lib.mkOption {
      type = lib.types.ints.positive;
      default = 3;
      description = "Maximum number of jobs building at the same time across all repositories.";
    };

    globCap = lib.mkOption {
      type = lib.types.ints.positive;
      default = 200;
      description = "Maximum total glob matches per build event.";
    };

    retention = lib.mkOption {
      type = lib.types.ints.positive;
      default = 1000;
      description = "Newest jobs kept per repository.";
    };

    publicUrl = lib.mkOption {
      type = lib.types.nullOr lib.types.str;
      default = null;
      example = "https://freezer.example.com";
      description = "The service's own HTTP(S) address, used only as the commit status target URL. Omitted from the generated configuration when null.";
    };

    githubApiBase = lib.mkOption {
      type = lib.types.str;
      default = "https://api.github.com";
      description = "GitHub API base URL. Mainly a test seam.";
    };

    cache = {
      bucket = lib.mkOption {
        type = lib.types.str;
        example = "public-nix-cache";
        description = "One S3 bucket, flat namespace, without a prefix.";
      };

      endpoint = lib.mkOption {
        type = lib.types.str;
        example = "https://ACCOUNT_ID.r2.cloudflarestorage.com";
        description = "HTTP(S) endpoint of the S3-compatible API, without credentials, query or fragment.";
      };

      signingKey = lib.mkOption {
        type = lib.types.str;
        default = "${stateDir}/signing.key";
        description = ''
          Absolute runtime path to the ed25519 binary-cache signing key. Nix
          opens it when copying; Freezer never reads it. Keep it outside the
          Nix store, readable by the `freezer` user.
        '';
      };
    };

    repositories = lib.mkOption {
      type = lib.types.listOf (lib.types.submodule {
        options = {
          name = lib.mkOption {
            type = lib.types.strMatching "[A-Za-z0-9][A-Za-z0-9_-]*";
            description = "Unique safe path component used for the checkout directory and URLs.";
          };
          url = lib.mkOption {
            type = lib.types.str;
            example = "https://github.com/OWNER/REPOSITORY.git";
            description = "Git remote URL. Register public repositories only.";
          };
          github = lib.mkOption {
            type = lib.types.strMatching "[A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+";
            example = "OWNER/REPOSITORY";
            description = "GitHub owner/name used for commit statuses.";
          };
        };
      });
      default = [ ];
      description = "Repositories whose default branches are polled and built. At least one is required.";
    };
  };

  config = lib.mkIf cfg.enable {
    assertions = [
      {
        assertion = lib.hasPrefix "/" cfg.environmentFile;
        message = "services.freezer.environmentFile must be an absolute runtime path.";
      }
      {
        assertion = lib.hasPrefix "/" cfg.cache.signingKey;
        message = "services.freezer.cache.signingKey must be an absolute runtime path.";
      }
      {
        assertion = cfg.repositories != [ ];
        message = "services.freezer.repositories must register at least one public repository.";
      }
      {
        assertion = config.nix.enable && lib.versionAtLeast config.nix.package.version "2.33";
        message = ''
          Freezer needs nix.enable = true and a local Nix daemon at version
          2.33 or newer. Set nix.package to a newer Nix, or disable
          services.freezer.
        '';
      }
    ];

    users.groups.freezer = { };
    users.users.freezer = {
      isSystemUser = true;
      group = "freezer";
      home = stateDir;
    };

    systemd.services.freezer = {
      description = "Freezer Nix flake build service";
      wantedBy = [ "multi-user.target" ];
      wants = [ "network-online.target" ];
      after = [ "network-online.target" "nix-daemon.socket" ];
      path = [ pkgs.git config.nix.package ];
      unitConfig.ConditionPathExists = cfg.environmentFile;
      environment = {
        HOME = stateDir;
        XDG_CACHE_HOME = "${stateDir}/cache";
        GIT_TERMINAL_PROMPT = "0";
        AWS_EC2_METADATA_DISABLED = "true";
      };
      serviceConfig = {
        Type = "simple";
        User = "freezer";
        Group = "freezer";
        StateDirectory = "freezer";
        StateDirectoryMode = "0700";
        WorkingDirectory = stateDir;
        ExecStart = utils.escapeSystemdExecArgs [ (lib.getExe cfg.package) configFile ];
        EnvironmentFile = cfg.environmentFile;
        Restart = "on-failure";
        RestartSec = 5;
        TimeoutStopSec = 10;
        UMask = "0077";
        NoNewPrivileges = true;
        PrivateTmp = true;
        ProtectSystem = "strict";
        ProtectHome = true;
      };
    };
  };
}
