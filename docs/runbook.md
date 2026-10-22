# Operating Freezer

Freezer publishes trusted, public Nix store paths. Register only public repositories whose default-branch code you trust. The web UI has no authentication: protect it with the reverse proxy or bind to localhost and use an SSH tunnel. Nothing needs to be reachable from outside: triggering is polling (ADR 0024), so the whole service can stay on a private network.

## Install on NixOS

The flake exports `nixosModules.default`, which installs the packaged binary and runs it as the `freezer` service against the host's Nix daemon. The module creates the `freezer` user and group, owns `/var/lib/freezer` with systemd's `StateDirectory` (mode 0700), puts git and `config.nix.package` on the unit's PATH, and applies the unit hardening. It asserts that `nix.enable` is set and the host Nix is at least 2.33; set `nix.package` to a newer Nix if the default is older.

Add the flake as an input and import the module:

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
            environmentFile = "/run/secrets/freezer-environment";
            publicUrl = "https://freezer.example.com";
            cache = {
              bucket = "public-nix-cache";
              endpoint = "https://ACCOUNT_ID.r2.cloudflarestorage.com";
            };
            repositories = [
              {
                name = "personal-flake";
                url = "https://github.com/OWNER/REPOSITORY.git";
                github = "OWNER/REPOSITORY";
              }
            ];
          };
        }
      ];
    };
  };
}
```

The module generates the server YAML from these options and passes the generated file to the binary, so there is no YAML to write. `environmentFile` is a runtime path string, not a Nix path, so the credentials never enter the store; the generated configuration contains only public repository names, URLs, the cache endpoint and the signing-key path. `listen` defaults to `127.0.0.1:8080`, `pollInterval` to `15m`, `jobTimeout` to `1h`, `globalBuildCap` to 3, `globCap` to 200, `retention` to 1000, and `cache.signingKey` to `/var/lib/freezer/signing.key`.

Apply the configuration, then prepare the runtime files and start the service. The unit carries `ConditionPathExists` for the environment file, so the first `nixos-rebuild switch` leaves it inactive instead of restart-looping:

```sh
nixos-rebuild switch
install -d -o freezer -g freezer -m 0700 /var/lib/freezer
install -d -m 0755 /run/secrets
install -o root -g root -m 0600 deploy/freezer.example.env /run/secrets/freezer-environment
umask 077
nix-store --generate-binary-cache-key freezer-cache-1 ./freezer-cache.key ./freezer-cache.pub
install -o freezer -g freezer -m 0400 ./freezer-cache.key /var/lib/freezer/signing.key
systemctl start freezer
```

Edit the environment file before starting: `GITHUB_TOKEN` and the R2 credentials. `systemd` reads it as root before dropping to `freezer`, so root ownership with mode 0600 is correct.

**Boot and crashes.** The unit is `wantedBy=multi-user.target`, so it starts at boot, and `Restart=on-failure` restarts it after an unexpected exit. A deliberate `systemctl stop freezer` stays stopped. `TimeoutStopSec=10` bounds shutdown; Freezer closes its listener and cancels running work itself.

**Triggering.** Polling only: `pollInterval` is positive (default `15m`). The service resolves every registered repository's default-branch head at start and on each interval afterwards, and builds whenever the head is not the commit last built. Nothing inbound is needed — no public route, no webhook, no reverse-proxy vhost — so the UI can stay on a private network or behind an SSH tunnel and the deployment can be unreachable from the internet. A poll that cannot reach the remote shows up as a failed repository state in the UI and posts no commit status. Ten minutes is a reasonable start; a poll costs one branch resolution per repository.

**Runtime behavior.**

- The host's Nix store holds the builds and published paths; Freezer writes only `/var/lib/freezer`, which survives rebuilds and reboots. R2 holds the durable copy of every published path.
- Builds run through the host's Nix daemon. If the host enables `nix.settings.sandbox` (the NixOS default), builds are sandboxed by the host's Nix; the module does not rewrite the daemon's settings.
- The service runs as the unprivileged `freezer` user. It needs no `trusted-users` entry for `nix eval`, `nix path-info` and `nix copy`; adding one is a separate admin decision. If `nix.settings.allowed-users` is narrowed from its default `*`, include `freezer` there. Hand-run repair commands may need a trusted user, as noted below.
- The module asserts Nix ≥ 2.33 (ADR 0017). A direct binary run outside the module has no such check.

Inspect and operate:

```sh
systemctl status freezer
journalctl -u freezer --since today
systemctl restart freezer
```

Upgrades are `nixos-rebuild switch`: the package path changes, systemd restarts the unit, and `/var/lib/freezer` survives. A restart loses queued events and marks a running job interrupted; the last-built record survives.

## Runtime files and external services

The server settings are the `services.freezer` options in [README.md](../README.md#running-the-service); the module generates the YAML. Before starting the service, provide:

| File / setting | Contents and access |
|---|---|
| Generated server YAML | Written by the module to the Nix store from the options, and passed to the binary. Public data only; it is not edited by hand. |
| Signing key | An ed25519 binary-cache key, mode 0400, owned by `freezer`, outside the Nix store. Back it up separately. Freezer never reads its contents; Nix opens it when copying. |
| Environment file | `GITHUB_TOKEN`, `AWS_ACCESS_KEY_ID`, `AWS_SECRET_ACCESS_KEY`; root:root mode 0600. systemd reads it as root and supplies the values to the service. |
| R2 bucket | One bucket, flat namespace. The S3 endpoint is `https://ACCOUNT_ID.r2.cloudflarestorage.com`; credentials need bucket read/write access. |
| Public custom domain | Read-only access to that same bucket. This URL is the consumer substituter, not Freezer's HTTP address. |
| GitHub token | Permission to create commit statuses on every registered repository. Status context is `freezer`; this is the only thing the service sends to GitHub. |

Generate a new signing key only when setting up a new trust identity, on any host with Nix installed:

```sh
umask 077
nix-store --generate-binary-cache-key freezer-cache-1 ./freezer-cache.key ./freezer-cache.pub
```

Keep the private file at the configured path (`/var/lib/freezer/signing.key` by default) and nowhere else. Distribute only `freezer-cache.pub` to consumers. Never put either private credentials or the signing key into a flake expression, source commit or database.

Cloudflare's public-domain setup is documented at [R2 public buckets](https://developers.cloudflare.com/r2/buckets/public-buckets/#custom-domains). Freezer does not serve the bucket or manage its domain. Configure consumer Nix with the public URL in `extra-substituters` and the public key in `extra-trusted-public-keys`.

Do not print the environment file into shared transcripts. Open the authenticated UI, press Build now, and inspect the job's plain-text transcript if publication fails. There are no operational CLI subcommands.

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

Deleting only a nar does **not** repair anything: both a normal probe and `nix copy` can succeed while its narinfo remains. Build now bypasses the probe, but it cannot make Nix recheck an existing narinfo's nar. Delete the matching narinfo as well. Any additional hand-run `nix path-info`, `nix copy` or substitution command must include the two TTL options above — and on a host whose Nix does not trust the invoking user, those options are *ignored* with a warning (the default positive TTL is 30 days), so hand-run verification can report a deleted object as present. Run hand-run repair commands as a trusted user, normally root, or add the invoking account to `nix.settings.trusted-users` deliberately.

If the public domain still returns stale metadata after the bucket is repaired, check the domain's cache policy and invalidate the specific stale response there. Freezer's own probe uses the S3 API and cannot verify what the public domain serves. Re-run the scratch-store substitution through the domain before considering the repair complete.

## Self-hosting

Register `https://github.com/pelikanade/freezer.git` with GitHub slug `pelikanade/freezer`, using the implementation revision intended for deployment. This repository's `.freezer.yaml` declares `packages.x86_64-linux.default`. On an aarch64 host, change the declared entry to `packages.aarch64-linux.default` in the public repository, or supply an appropriate remote builder.

After one successful publication, verify a cache-hit event with a subsequent default-branch commit that changes only documentation. The package source excludes documentation, so the output path remains the same. A redelivery of the **same** successful commit correctly creates no jobs; Build now deliberately skips the probe and therefore cannot demonstrate a cache hit.

## Retention, backups and restarts

`retention` keeps the newest N jobs per repository. Startup applies it to every recorded repository; each finished event applies it to that repository. Running jobs are never deleted. Job rows and full transcripts disappear together; last-built state does not. Lowering the setting and restarting is sufficient to apply a smaller history bound.

Stop the service for a file-copy backup (`systemctl stop freezer`), or use SQLite's online backup mechanism against `/var/lib/freezer/freezer.sqlite`. Copying only `freezer.sqlite` while WAL writes are active is not a valid file-copy backup; use the backup API or stop the service so the WAL is checkpointed. Back up the signing key separately. Checkouts under `/var/lib/freezer/checkouts/` and the host's Nix store are not retained or garbage-collected by Freezer.

**Clearing the history.** Retention always keeps the newest N jobs, so it cannot empty the list. To start over — after a run of interrupted or failed jobs from a bad release, say — stop the service and delete `freezer.sqlite` (with its `-wal` and `-shm` files) from `/var/lib/freezer`; the database is recreated at the next start, repository rows included. That also discards the last-built record, so the next poll builds the head again, and it drops every transcript.

SQLite deletion reuses pages but does not shrink the database file. An admin may run `VACUUM` during a maintenance window after taking a backup. Do not mistake a constant file size for failed retention: check that the old job page and `/jobs/ID/log` both return 404 while the newest jobs remain readable.

Queued events are lost on restart. A running job becomes interrupted and its commit is not recorded; any transcript already written survives. Missed pushes and interrupted publication require Build now or a later push. There is no reconcile sweep or automatic job retry.

## Real-host acceptance record

These five checks are intentionally **not marked complete** by local tests. Fill each row with the designated host, deployed revision and current execution evidence. R2 credentials and private keys do not belong in this record.

| Roadmap criterion | Current result | Required evidence |
|---|---|---|
| Service installed; correct state, key and environment | **Done 2026-10-15**, host `neko-sphere`: the service ran on `127.0.0.1:8080`, wrote state under `/var/lib/freezer`, read the signing key from its protected path, and took its environment from the environment file. The poll at start resolved `b23475c7` and posted a `pending` status with context `freezer`, verified through the GitHub API. | Host, deployed revision, project state, readable key path and successful service start. |
| Real R2 publish and public-domain substitution | **Publish done 2026-10-15**: 685 objects in `nix-binary-cache`; `/nix/store/lqwdwbyvx8xra6iw94jplsa7naza81gm-agent-orchestrator-0.13.5.narinfo` carries `Compression: zstd` and `Sig: freezer-cache-1:I4CM0/3R6QBIh6W8/23Z6i3yGLlpz4shYx9CTtWJn+J29kwrK4CbL6TG28Vt4RDRAuDmtGijGHqKaz4HKYr6DQ==`; `nix copy --from` the bucket's S3 endpoint into a fresh store, then `nix store verify --sigs-needed 1` with the public key, returned 0 over the closure. Substitution **through the public domain** still waits for a custom domain. | Published path and successful fresh-store copy plus hash/signature verification through the public domain. |
| Freezer builds itself; next event is a cache hit | Cache **hit** demonstrated on real data: `agent-orchestrator-0.13.5`, published by one event, was `cached — nothing built` in the next event, its probe answered by the bucket. Self-hosting — registering this repository — is not configured yet. | Both event commits, job IDs and transcripts. Same-commit dedup is not this check. |
| Real-host retention deletes old jobs and transcripts | Not demonstrated on real data: `retention` is 1000 and nothing has aged out. The history was cleared once by hand instead (see "Clearing the history"). | Retention value, old job/page 404 and retained job/page 200 after a real event or restart. |
| Real repair recipe succeeds | Waiting for a designated test object. | Exact deleted keys, Build now job, and successful fresh-store substitution afterwards. |

Local verification is tracked in [ROADMAP.md](../ROADMAP.md) and the owner's gitignored project map. It proves the implementation against Silo and a GitHub HTTP stand-in. The record above now covers the real bucket, the real R2 credentials and the real GitHub token; a custom domain, retention on aged data, and the repair recipe are still open.

The local self-hosting smoke also compiled a temporary clone of the final implementation through the packaged service, verified a documentation-only follow-up as a cache hit, and substituted the result into a fresh store with hash/signature checks. A comment in the temporary clone forced a new derivation; no product behavior was changed for the smoke. The service used its own HOME and did not inherit the developer's `XDG_CONFIG_HOME` Git filters, matching the module's service environment. Raw transcripts and exact fixture revisions are retained in `.project-map/`.

For hand-run flake and store commands, enable `nix-command` and `flakes` or pass `--extra-experimental-features 'nix-command flakes'`. Freezer supplies those features per invocation and leaves the host configuration alone.
