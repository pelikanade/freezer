# Operating Freezer

Freezer publishes trusted, public Nix store paths. Register only public repositories whose default-branch code you trust. The web UI has no authentication: protect it with the reverse proxy or bind to localhost and use an SSH tunnel. Only the signed webhook route needs public access.

## Install with NixOS

The flake exposes `packages.<system>.default`, `packages.<system>.freezer` and `nixosModules.default` for x86_64-linux and aarch64-linux. Build locally with `nix build`; `devenv test` runs the real Nix/Git/S3 acceptance suite. The package is a static Go executable with its templates and stylesheet embedded.

Add Freezer as a flake input and import its module:

```nix
{
  inputs.freezer.url = "github:pelikanade/freezer";

  outputs = { nixpkgs, freezer, ... }: {
    nixosConfigurations.build-host = nixpkgs.lib.nixosSystem {
      system = "x86_64-linux";
      modules = [
        ./configuration.nix
        freezer.nixosModules.default
        {
          services.freezer = {
            enable = true;
            configFile = "/run/secrets/freezer-config.yaml";
            environmentFile = "/run/secrets/freezer-environment";
          };
        }
      ];
    };
  };
}
```

These secret paths are strings, not Nix path literals. Provision their contents through the host's existing secret-management mechanism. The module does not copy secret files into the Nix store, create credentials, open firewall ports, configure a reverse proxy, or add the service user to Nix's trusted-users.

The module runs as `freezer:freezer`, creates `/var/lib/freezer` with mode 0700, sets HOME and the Nix client cache there, and supplies git plus the host's `config.nix.package` on PATH. It uses a private temporary directory and a read-only system view; state remains writable. Put signing keys and configuration under `/run/secrets` or `/var/lib/freezer`, not under a user's home. Set `state_dir: /var/lib/freezer` in the YAML. A stopped service has ten seconds to exit; jobs still marked running are recovered as interrupted at the next start.

Nix >= 2.33 is the design floor, with 2.34 tested. Use at least 2.33.6 when selecting the 2.33 series, since that is where the addressing-style option was backported. The module uses the host's Nix; Freezer does not validate or rewrite daemon settings. If the host rejects an option or a build, its Nix error is recorded in the job transcript.

## Runtime files and external services

Use the server configuration in [README.md](../README.md#running-the-service). Before starting the service, provide:

| File / setting | Contents and access |
|---|---|
| Server YAML | Registered public repositories, per-repository webhook secrets, cache endpoint and signing-key filename. Readable by `freezer`, not world-readable. |
| Signing key | An ed25519 binary-cache key, mode 0600, owned by `freezer`. Back it up separately. Freezer never reads its contents; Nix opens it when copying. |
| Environment file | `GITHUB_TOKEN`, `AWS_ACCESS_KEY_ID`, `AWS_SECRET_ACCESS_KEY`; root-readable mode 0600. systemd supplies the values to the service. |
| R2 bucket | One bucket, flat namespace. The S3 endpoint is `https://ACCOUNT_ID.r2.cloudflarestorage.com`; credentials need bucket read/write access. |
| Public custom domain | Read-only access to that same bucket. This URL is the consumer substituter, not Freezer's HTTP address. |
| GitHub webhook | Push events to `https://SERVICE/hooks/REPOSITORY_NAME`, with the matching repository secret and HTTPS. |
| GitHub token | Permission to create commit statuses on every registered repository. Status context is `freezer`. |

Generate a new signing key only when setting up a new trust identity:

```sh
umask 077
nix-store --generate-binary-cache-key freezer-cache-1 ./freezer-cache.key ./freezer-cache.pub
```

Move the private file to the configured protected location using the host's secret-management mechanism. Distribute only `freezer-cache.pub` to consumers. Never put either private credentials or the signing key into a flake expression, source commit or database.

Cloudflare's public-domain setup is documented at [R2 public buckets](https://developers.cloudflare.com/r2/buckets/public-buckets/#custom-domains). Freezer does not serve the bucket or manage its domain. Configure consumer Nix with the public URL in `extra-substituters` and the public key in `extra-trusted-public-keys`.

After applying the module, inspect the actual unit and its state on the designated host:

```sh
systemctl status freezer
systemctl show freezer -p User -p Group -p WorkingDirectory -p StateDirectory -p EnvironmentFiles
journalctl -u freezer --since today
```

Do not print the environment file or private config into shared transcripts. Open the authenticated UI, press Build now, and inspect the job's plain-text transcript if publication fails. There are no operational CLI subcommands.

## Verify substitution through the public domain

Use a published output path from a successful job. Every hand-run cache command must disable both narinfo TTLs; otherwise a deleted object can remain apparently valid for 30 days.

```sh
FREEZER_PUBLIC_CACHE='https://cache.example.com'
FREEZER_PUBLIC_KEY='freezer-cache-1:PUBLIC_KEY_FROM_THE_PUB_FILE'
FREEZER_STORE_PATH='/nix/store/HASH-NAME_FROM_THE_JOB'
FREEZER_SCRATCH="$(mktemp -d)"
freezer_fresh=(--option narinfo-cache-positive-ttl 0 --option narinfo-cache-negative-ttl 0)

nix copy --from "$FREEZER_PUBLIC_CACHE" --to "local?root=$FREEZER_SCRATCH" \
  --option trusted-public-keys "$FREEZER_PUBLIC_KEY" \
  "${freezer_fresh[@]}" "$FREEZER_STORE_PATH"
nix store verify --store "local?root=$FREEZER_SCRATCH" --sigs-needed 1 \
  --option trusted-public-keys "$FREEZER_PUBLIC_KEY" \
  "${freezer_fresh[@]}" "$FREEZER_STORE_PATH"
```

The scratch store ensures the local build cannot satisfy the check. The copy and verification must succeed with the expected key; do not bypass signatures. Record the domain, output path, commit and result below, without credentials. Afterwards, remove only the scratch directory created above, making its read-only Nix directories writable first.

## Repair a published object

Identify the exact affected store path and bucket keys before deleting anything. Perform a repair exercise on an explicitly selected test output. Deleting unrelated objects or emptying the bucket is not part of this procedure.

A store path's narinfo key is its hash prefix followed by `.narinfo`. Read its public narinfo and preserve the `URL:` field before deleting it; that field identifies the corresponding nar object. Use the R2 dashboard or an authenticated object-store client for the deletion. Freezer intentionally has no delete or repair API.

| Failure | Delete | Then |
|---|---|---|
| Broken or missing narinfo | That narinfo object | Press Build now for its repository, then substitute into a fresh scratch store. |
| Broken or missing nar | Both its narinfo and the nar named by `URL:` | Press Build now, then substitute and verify. |
| Broken closure member | The affected member's narinfo, plus its nar if damaged | Press Build now for the top-level entry. The ordinary probe only sees the top-level path. |
| Interrupted publication | Identify the missing/damaged closure member and use the applicable row above | Build now bypasses the top-level probe and copies missing members. |

Deleting only a nar does **not** repair anything: both a normal probe and `nix copy` can succeed while its narinfo remains. Build now bypasses the probe, but it cannot make Nix recheck an existing narinfo's nar. Delete the matching narinfo as well. Any additional hand-run `nix path-info`, `nix copy` or substitution command must include the two TTL options above.

If the public domain still returns stale metadata after the bucket is repaired, check the domain's cache policy and invalidate the specific stale response there. Freezer's own probe uses the S3 API and cannot verify what the public domain serves. Re-run the scratch-store substitution through the domain before considering the repair complete.

## Self-hosting

Register `https://github.com/pelikanade/freezer.git` with GitHub slug `pelikanade/freezer`, using the implementation revision intended for deployment. This repository's `.freezer.yaml` declares `packages.x86_64-linux.default`. On an ARM-only host, change the declared entry to `packages.aarch64-linux.default` in the public repository, or supply an appropriate remote builder.

After one successful publication, verify a cache-hit event with a subsequent default-branch commit that changes only documentation. The package source excludes documentation, so the output path remains the same. A redelivery of the **same** successful commit correctly creates no jobs; Build now deliberately skips the probe and therefore cannot demonstrate a cache hit.

## Retention, backups and restarts

`retention` keeps the newest N jobs per repository. Startup applies it to every recorded repository; each finished event applies it to that repository. Running jobs are never deleted. Job rows and full transcripts disappear together; last-built state does not. Lowering the setting and restarting is sufficient to apply a smaller history bound.

Stop the service for a file-copy backup, or use SQLite's online backup mechanism. Copying only `freezer.sqlite` while WAL writes are active is not a valid file-copy backup; use the backup API or stop the service so the WAL is checkpointed. Back up the signing key separately. Checkouts and the host store are not retained or garbage-collected by Freezer.

SQLite deletion reuses pages but does not shrink the database file. An admin may run `VACUUM` during a maintenance window after taking a backup. Do not mistake a constant file size for failed retention: check that the old job page and `/jobs/ID/log` both return 404 while the newest jobs remain readable.

Queued events are lost on restart. A running job becomes interrupted and its commit is not recorded; any transcript already written survives. Missed pushes and interrupted publication require Build now or a later push. There is no reconcile sweep or automatic job retry.

## Real-host acceptance record

These five checks are intentionally **not marked complete** by local tests. Fill each row with the designated host, deployed revision and current execution evidence. R2 credentials and private keys do not belong in this record.

| Roadmap criterion | Current result | Required evidence |
|---|---|---|
| Module applied; correct user, state, key and environment | Waiting for deployment target | Host, revision, unit properties, readable key path and successful service start. |
| Real R2 publish and public-domain substitution | Waiting for bucket/domain and credentials | Published path and successful fresh-store copy plus hash/signature verification through the public domain. |
| Freezer builds itself; next event is a cache hit | Waiting for deployed implementation and registered repository | Both event commits, job IDs and transcripts. Same-commit dedup is not this check. |
| Real-host retention deletes old jobs and transcripts | Waiting for deployment target | Retention value, old job/page 404 and retained job/page 200 after a real event or restart. |
| Real repair recipe succeeds | Waiting for a designated test object | Exact deleted keys, Build now job, and successful fresh-store substitution afterwards. |

Local verification is tracked in [ROADMAP.md](../ROADMAP.md) and the owner's gitignored project map. It proves the implementation against Silo and a GitHub HTTP stand-in; it does not prove Cloudflare, a custom domain or the real GitHub token.

The local self-hosting smoke also compiled a temporary clone of the final implementation through the packaged service, verified a documentation-only follow-up as a cache hit, and substituted the result into a fresh store with hash/signature checks. A comment in the temporary clone forced a new derivation; no product behavior was changed for the smoke. The service used its own HOME and did not inherit the developer's `XDG_CONFIG_HOME` Git filters, matching the module's separate-account environment. Raw transcripts and exact fixture revisions are retained in `.project-map/`.

For hand-run flake and store commands, enable `nix-command` and `flakes` or pass `--extra-experimental-features 'nix-command flakes'`. Freezer supplies those features per invocation and leaves the host configuration alone.
