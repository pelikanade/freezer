# Deployment is the NixOS module

Freezer is deployed by importing `nixosModules.default` from its flake into a NixOS configuration. The flake also exports `packages.<system>.freezer` (and `default`) for the same binary, but the module is the supported deployment: it installs that package and runs it as the `freezer` systemd service against the host's Nix daemon.

The module owns the service's runtime shape:

- a `freezer` system user and group, with `StateDirectory=freezer` at `/var/lib/freezer` (mode 0700);
- `HOME` and `XDG_CACHE_HOME` in that state directory, `GIT_TERMINAL_PROMPT=0` and `AWS_EC2_METADATA_DISABLED=true`;
- git and the host's `config.nix.package` on `PATH`, so the service invokes the same Nix the host uses for everything else;
- restart on failure, a ten-second stop timeout, and the hardening a service that writes only its state directory and talks to the Nix daemon needs: `NoNewPrivileges`, `PrivateTmp`, `ProtectSystem=strict`, `ProtectHome`, `UMask=0077`;
- an assertion that `nix.enable` and `config.nix.package.version` are at least Nix 2.33 (ADR 0017). The module does not rewrite daemon settings, add the service user to `trusted-users`, create credentials, or configure a reverse proxy.

The module declares the server settings as typed NixOS options (`listen`, `pollInterval`, `jobTimeout`, `globalBuildCap`, `globCap`, `retention`, `publicUrl`, `githubApiBase`, `cache.*`, `repositories`), fixes `state_dir` to `/var/lib/freezer`, and generates the binary's YAML with `pkgs.formats.yaml`. The generated file lives in the Nix store and is passed directly to the binary; no YAML is written by hand. It contains only public data — repository names and URLs, the cache endpoint and the signing-key path. `services.freezer.environmentFile` is the only runtime configuration file, holding `GITHUB_TOKEN`, `AWS_ACCESS_KEY_ID` and `AWS_SECRET_ACCESS_KEY`, so credentials never enter the store. The binary's `LoadConfig` still validates the generated file at startup; the typed options add evaluation-time checking of the same shape.

## Consequences

- The host runs NixOS with a local Nix daemon; `nixos-rebuild switch` installs the binary, regenerates the configuration and restarts the service when its package or options change.
- Nix does every build, probe and copy (ADR 0010). The version floor is a property of the host daemon, enforced by the module's assertion and recorded in the README; a direct binary run outside the module is on its own.
- Builds run in the host store through the daemon, so store garbage collection remains the host's concern. `/var/lib/freezer` holds only SQLite state and the persistent checkouts; the signing key is a separate runtime file outside the store.
- The service user is unprivileged and not trusted by the daemon. The service does not need trust for its own `nix eval`, `nix path-info` and `nix copy` invocations; an operator who also wants hand-run repair commands' narinfo TTL options honoured must run those as a trusted user.
- The UI binds loopback by default. A reverse proxy or SSH tunnel provides TLS and access control; triggering is polling, so no route needs to be public (ADR 0024).
- State survives `nixos-rebuild switch` and reboot. A file-copy backup is taken with the unit stopped, and the signing key is backed up separately.
- Adding a setting to the binary's `Config` means adding a module option and emitting it in the generated YAML. The two schemas are kept in step by review; `LoadConfig` remains the final check.

## Considered Options

- **A hand-written server YAML.** Rejected: the deployment would grow a second configuration surface beside the NixOS options, with no evaluation-time validation, and the operator would have to copy an example and keep its paths in sync with the unit.
- **A freeform `settings` attribute set.** Rejected: it would accept any key and type, so a misspelled bucket or repository field would reach the service and fail only at startup. Typed options name every accepted setting.
- **Putting credentials in the generated YAML.** Rejected: that would put `GITHUB_TOKEN` and the R2 keys in the Nix store, readable by every user on the host. They stay in `environmentFile`.
- **A container deployment.** Rejected for a NixOS-only service: it carries a second Nix and its own store, adds a container runtime and image-build pipeline, and makes the host's daemon irrelevant. The host already provides a pinned, trusted Nix and systemd.
- **A dynamic user.** Rejected: the state directory, checkouts and signing key must persist and be owned predictably, and the service needs a stable identity for the host's Nix access.
- **Running as root.** Not needed: the daemon performs builds, and an unprivileged service account can read the checkout, talk to the daemon socket and upload to the S3 store with its own credentials.
