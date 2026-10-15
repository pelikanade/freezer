# Operating Freezer

Freezer publishes trusted, public Nix store paths. Register only public repositories whose default-branch code you trust. The web UI has no authentication: protect it with the reverse proxy or bind to localhost and use an SSH tunnel. Nothing needs to be reachable from outside: triggering is polling (ADR 0024), so the whole service can stay on a private network.

## Deploy as a container

One container under rootless Podman, with no Nix on the host. The image is built from the `Dockerfile` in the repository root: its first stage compiles the Go package with the official Go image, and the result runs on the official Nix image (`nixos/nix:2.34.8`), which ships Nix 2.34.8, git, tar and curl on `PATH` — everything Freezer shells out to. Building the image runs no Nix. The service drives the Nix *inside* the container, so builds, probing and publication all happen there and the host's Nix daemon is never used. (`devenv test` is the development acceptance suite and still needs the host daemon.)

```sh
podman build -t freezer:0.1.0 .
```

Everything the service writes lives under the deploying user's home, and the container runs as that user. Prepare the deployment directory and bring the project up:

```sh
mkdir -p ~/freezer/state
cp deploy/freezer.example.yaml ~/freezer/freezer.yaml   # bucket, endpoint, repositories
install -m 600 deploy/.env.example ~/freezer/.env       # then edit: GITHUB_TOKEN and the R2 credentials
chmod 600 ~/freezer/freezer.yaml
cp deploy/compose.yaml ~/freezer/compose.yaml
podman run --rm --entrypoint nix-store -v "$HOME/freezer:/keys" freezer:0.1.0 \
  --generate-binary-cache-key freezer-cache-1 /keys/signing.key /keys/signing.pub
podman compose -f ~/freezer/compose.yaml up -d
```

`deploy/compose.yaml` is the whole deployment, pinned in one file: the image tag, the loopback port, the three bind mounts, the environment file, the health check, `restart: always`, and the two sandbox settings below. Every path in it is relative, which is why the file belongs in `~/freezer/` beside the configuration, the key and the state directory. `docker compose` reads the same file.

The configuration must set `listen: 0.0.0.0:8080` and `state_dir: /var/lib/freezer`, and must name the signing key where the container sees it — `/etc/freezer/signing.key`. Its keys are the ones in [README.md](../README.md#running-the-service); `deploy/freezer.example.yaml` is a working starting point. `~/freezer/signing.key` is the only copy of the private key: back it up, and hand only `signing.pub` to consumers.

**Boot and crashes.** `restart: always` restarts the container whenever it dies, and a deliberate `podman compose stop` stays stopped. To bring it back after a reboot, enable the user unit that re-runs containers carrying a restart policy: `systemctl --user enable --now podman-restart.service`, together with lingering (`loginctl enable-linger "$USER"`, or `users.users.<name>.linger = true` on NixOS) so it runs without a login. On a host that enforces SELinux, add `,Z` to the three volume options in the compose file.

**Triggering.** Polling only: `poll_interval` is required and positive. The service resolves every registered repository's default-branch head at start and on each interval afterwards, and builds whenever the head is not the commit last built. Nothing inbound is needed — no public route, no webhook, no reverse-proxy vhost — so the UI can stay on a private network or behind an SSH tunnel and the deployment can be unreachable from the internet. A poll that cannot reach the remote shows up as a failed repository state in the UI and posts no commit status. Ten minutes is a reasonable start; a poll costs one branch resolution per repository.

**Runtime behavior.**

- The container's Nix store is its writable layer: it survives stop, start and reboot of the same container, and is discarded when the container is recreated (a new image tag, or an edited compose file). R2 holds the published paths, so a discarded store costs rebuild time on the next push, never re-publication, and nothing else is lost. `state_dir` is the bind mount and does survive recreation.
- Builds are sandboxed: the image sets `sandbox = true`, and the compose file hands the container the two things Nix's sandbox needs — `cap_add: [SYS_ADMIN]` and `security_opt: [unmask=ALL]`. **Neither is optional, and a stock container has neither.** Podman drops `CAP_SYS_ADMIN` from the container's capability set, so without it Nix cannot create the mount, PID and UTS namespaces a build runs in; and Nix probes the sandbox by *remounting `/proc`*, which the kernel refuses while Podman has paths masked inside it, so the probe fails on a default container even with the capability added. Both failures look identical — every build stops with "this system does not support the kernel namespaces that are required for sandboxing" — because `sandbox-fallback = false` is set in the image. That is the deliberate trade: loud, not silent. The escape hatch is a `nix.conf` mounted at `/etc/nix/nix.conf` with `sandbox = false`, and it is a poor one: without a sandbox, Nix requires `/homeless-shelter` — the `HOME` it gives builders — to not exist, and a builder that writes into its home directory creates it, after which *every later build in that container* fails with "home directory /homeless-shelter exists; please remove it". Removing that directory is the recovery (`podman exec freezer rm -rf /homeless-shelter`). Disabling the sandbox also removes it from the trust story: a build then runs with the container's environment, which holds the R2 credentials and the GitHub token.
- Build users: the image sets an empty `build-users-group`, so there is one single-user store inside the container and builds run as the container's user inside the sandbox. Nothing depends on the deploying host's subuid range beyond the mapping rootless Podman already requires, and no build user exists to be mapped.
- Nix 2.34.8 is pinned in the image, so the 2.33 floor (ADR 0017) holds by construction. A hand-built image is on its own.

Inspect and operate:

```sh
podman compose -f ~/freezer/compose.yaml ps
podman logs freezer
podman healthcheck run freezer
```

Upgrades are a rebuild and a recreate: build the new tag (`podman build -t freezer:0.2.0 .`), point `image:` at it in `~/freezer/compose.yaml`, and run `podman compose -f ~/freezer/compose.yaml up -d` again. Compose recreates the container — which discards the container's own Nix store, the trade noted above, while `state_dir` is a bind mount and survives — and `restart: always` keeps it up.

## Runtime files and external services

Use the server configuration in [README.md](../README.md#running-the-service). Before starting the service, provide:

| File / setting | Contents and access |
|---|---|
| Server YAML | Registered public repositories, cache endpoint and signing-key filename. Mounted read-only at `/etc/freezer/freezer.yaml`; mode 0600 on the host. |
| Signing key | An ed25519 binary-cache key, mode 0600, owned by the deploying user. Back it up separately. Freezer never reads its contents; Nix opens it when copying. |
| Environment file (`.env`) | `GITHUB_TOKEN`, `AWS_ACCESS_KEY_ID`, `AWS_SECRET_ACCESS_KEY`; mode 0600, gitignored, from `deploy/.env.example`. Compose passes it with `env_file`, so the values exist only in the container's environment. |
| R2 bucket | One bucket, flat namespace. The S3 endpoint is `https://ACCOUNT_ID.r2.cloudflarestorage.com`; credentials need bucket read/write access. |
| Public custom domain | Read-only access to that same bucket. This URL is the consumer substituter, not Freezer's HTTP address. |
| GitHub token | Permission to create commit statuses on every registered repository. Status context is `freezer`; this is the only thing the service sends to GitHub. |

Generate a new signing key only when setting up a new trust identity — in the container, as above, or anywhere Nix is installed:

```sh
umask 077
nix-store --generate-binary-cache-key freezer-cache-1 ./freezer-cache.key ./freezer-cache.pub
```

Keep the private file where the compose file mounts it (`~/freezer/signing.key`) and nowhere else. Distribute only `freezer-cache.pub` to consumers. Never put either private credentials or the signing key into a flake expression, source commit or database.

Cloudflare's public-domain setup is documented at [R2 public buckets](https://developers.cloudflare.com/r2/buckets/public-buckets/#custom-domains). Freezer does not serve the bucket or manage its domain. Configure consumer Nix with the public URL in `extra-substituters` and the public key in `extra-trusted-public-keys`.

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

Deleting only a nar does **not** repair anything: both a normal probe and `nix copy` can succeed while its narinfo remains. Build now bypasses the probe, but it cannot make Nix recheck an existing narinfo's nar. Delete the matching narinfo as well. Any additional hand-run `nix path-info`, `nix copy` or substitution command must include the two TTL options above — and on a host whose Nix does not trust the invoking user, those options are *ignored* with a warning (the default positive TTL is 30 days), so hand-run verification can report a deleted object as present. The container deployment does not have that problem: Nix runs as root inside the container, where its own configuration trusts it.

If the public domain still returns stale metadata after the bucket is repaired, check the domain's cache policy and invalidate the specific stale response there. Freezer's own probe uses the S3 API and cannot verify what the public domain serves. Re-run the scratch-store substitution through the domain before considering the repair complete.

## Self-hosting

Register `https://github.com/pelikanade/freezer.git` with GitHub slug `pelikanade/freezer`, using the implementation revision intended for deployment. This repository's `.freezer.yaml` declares `packages.x86_64-linux.default`. On an aarch64 container, change the declared entry to `packages.aarch64-linux.default` in the public repository, or supply an appropriate remote builder.

After one successful publication, verify a cache-hit event with a subsequent default-branch commit that changes only documentation. The package source excludes documentation, so the output path remains the same. A redelivery of the **same** successful commit correctly creates no jobs; Build now deliberately skips the probe and therefore cannot demonstrate a cache hit.

## Retention, backups and restarts

`retention` keeps the newest N jobs per repository. Startup applies it to every recorded repository; each finished event applies it to that repository. Running jobs are never deleted. Job rows and full transcripts disappear together; last-built state does not. Lowering the setting and restarting is sufficient to apply a smaller history bound.

Stop the service for a file-copy backup (`podman compose -f ~/freezer/compose.yaml stop`), or use SQLite's online backup mechanism. Copying only `freezer.sqlite` while WAL writes are active is not a valid file-copy backup; use the backup API or stop the service so the WAL is checkpointed. Back up the signing key separately. Checkouts and the container's Nix store are not retained or garbage-collected by Freezer.

SQLite deletion reuses pages but does not shrink the database file. An admin may run `VACUUM` during a maintenance window after taking a backup. Do not mistake a constant file size for failed retention: check that the old job page and `/jobs/ID/log` both return 404 while the newest jobs remain readable.

Queued events are lost on restart. A running job becomes interrupted and its commit is not recorded; any transcript already written survives. Missed pushes and interrupted publication require Build now or a later push. There is no reconcile sweep or automatic job retry.

## Real-host acceptance record

These five checks are intentionally **not marked complete** by local tests. Fill each row with the designated host, deployed revision and current execution evidence. R2 credentials and private keys do not belong in this record.

| Roadmap criterion | Current result | Required evidence |
|---|---|---|
| Container installed; correct state, key and environment | Waiting for deployment target | Host, deployed image tag, unit state, readable key path and successful service start. |
| Real R2 publish and public-domain substitution | Waiting for bucket/domain and credentials | Published path and successful fresh-store copy plus hash/signature verification through the public domain. |
| Freezer builds itself; next event is a cache hit | Waiting for deployed implementation and registered repository | Both event commits, job IDs and transcripts. Same-commit dedup is not this check. |
| Real-host retention deletes old jobs and transcripts | Waiting for deployment target | Retention value, old job/page 404 and retained job/page 200 after a real event or restart. |
| Real repair recipe succeeds | Waiting for a designated test object | Exact deleted keys, Build now job, and successful fresh-store substitution afterwards. |

Local verification is tracked in [ROADMAP.md](../ROADMAP.md) and the owner's gitignored project map. It proves the implementation against Silo and a GitHub HTTP stand-in; it does not prove Cloudflare, a custom domain or the real GitHub token.

The local self-hosting smoke also compiled a temporary clone of the final implementation through the packaged service, verified a documentation-only follow-up as a cache hit, and substituted the result into a fresh store with hash/signature checks. A comment in the temporary clone forced a new derivation; no product behavior was changed for the smoke. The service used its own HOME and did not inherit the developer's `XDG_CONFIG_HOME` Git filters, matching the deployed container's environment. Raw transcripts and exact fixture revisions are retained in `.project-map/`.

For hand-run flake and store commands, enable `nix-command` and `flakes` or pass `--extra-experimental-features 'nix-command flakes'`. Freezer supplies those features per invocation and leaves the host configuration alone.
