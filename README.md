# Freezer

A self-hosted build service for nix flakes. It polls the registered GitHub repositories, builds what their build lists declare, and publishes the results to a public, signed, S3-compatible binary cache (Cloudflare R2), so consumers substitute them instead of rebuilding.

Status: implementation complete through the local v2 deliverables: build loop, service, Clearlooks UI, retention, flake package, container image and repair runbook. Real-host/R2 acceptance remains pending; see [ROADMAP.md](ROADMAP.md) and the [runbook](docs/runbook.md).

## Problem

Keep a single maintainer's public flakes pre-built. When the default branch of a registered GitHub repository moves, build the flake outputs that repository declares and publish them, signed, to one public cache behind a custom domain. Consumers add one substituter URL and one trusted public key, and that covers every registered repository.

It is for one admin running it on their own host for their own public repositories. It is not CI: no PR or pre-merge builds, no other forges, no users or auth of its own, no metrics.

## Shape

**Product surfaces**

- **Trigger input**: the poll interval. Every registered repository's default-branch head is resolved at start and on each interval afterwards, and a head that is not the commit last built starts an event. Nothing inbound is needed — there is no webhook route, no signature to check and no way in from outside the UI.
- **Web UI**, the only control surface: home (repositories and recent jobs), job page (details and transcript), and the Build now and Stop actions. There is no CLI. Anything the UI can't do is a config edit and a restart.
- **Server config file**, edited by the admin: registered repositories, cache target, signing key path, glob cap, global build cap, job timeout, retention, poll interval, and the GitHub token.
- **Build-list file** in each repository (`.freezer.yaml`): the repository's build list, and nothing else.

**External pieces**

- **GitHub**: commit statuses out (one shared service token). Nothing comes in: pushes are found by polling the branch head.
- **Nix** (≥ 2.33): does all building, publishing, and cache probing. The container image carries it; running the binary directly uses the host's daemon.
- **The S3-compatible bucket** (R2), read publicly through a custom domain. Freezer never serves the cache itself.
- **A reverse proxy** (or an SSH tunnel to a localhost bind) in front of the UI. It provides TLS and access control.

Terms are defined in [GLOSSARY.md](GLOSSARY.md). The reasons for each choice are in [docs/adr/](docs/adr/).

## Behavior

### Push, build, publish

A push is found, not delivered. The service resolves each registered repository's default-branch head at start and every `poll_interval` afterwards; a head that is not the commit last built starts a build event. Nothing arrives from outside, so there is no signature to check, no inbound route to expose and no way to lose a push: an event missed while the service was down is found by the next poll.

1. A poll resolves the repository's default-branch head through git (`git ls-remote --symref`). Repositories that are not registered are never resolved — registration is the whole allowlist.
2. The event goes into the repository's queue, which holds one waiting event. A newer event replaces it — except that nothing displaces a waiting Build now, whose reachability matters more than a coalesced poll — and a running event is never cancelled by a later one. One event runs per repository at a time, and different repositories run in parallel. A superseded event leaves no trace and gets no commit status. An event whose head is unchanged ends at the last-built check in step 3, so polling costs one resolution per repository per interval and nothing else.
3. The head is resolved again when the event starts, so a commit that a later push overtook while the event waited is never built: what gets built is whatever the branch points at then. If the resolved commit equals the last-built commit, the event ends there with no jobs.
4. Freezer posts a `pending` commit status on that resolved commit. It reads the repository's build-list file without evaluating any nix. A missing or unreadable file means `failure` and nothing gets built: reading config must never build anything.
5. Each entry becomes jobs — recorded as soon as the event starts, in build-list order, so the whole event is visible in the UI while it runs (ADR 0025). A job that has not taken a global build slot yet shows as *planned*, with no start time and no transcript; it becomes *stopped* if the event ends first, and *interrupted* if a restart gets there first:
   - **Glob expansion**: a glob entry lists the attribute names under its declared prefix, one level only, capped per event. Over the cap, every entry of that pattern fails with a clear message and none build; truncating silently would make the build list a lie. A prefix that doesn't exist fails that pattern's entries only, and other entries still build. A pattern that matches nothing is not an error, and its job is labelled *matched nothing*.
   - **Output path**: evaluate the entry's output path without building. Evaluation is never allowed to build (import-from-derivation is off here). A failed evaluation is not a job failure. It just counts as a miss.
   - **Probe**: ask the cache whether that path is published. Only a definite "published" answer is a cache hit, and the job stops there. Every other outcome is a miss. The probe may be wrong toward a miss (costing a rebuild), never toward a hit.
   - **Publish on a miss**: build if needed and copy the path to the cache, signed. This step decides whether an entry is buildable, so its error is the one the job reports. Import-from-derivation stays at the host default here, so an entry that needs it to evaluate still builds.
6. Final commit status, one per event:
   - `success`: every job succeeded, including when every entry was a cache hit (the commit is verified built even though nothing was published). The commit is recorded as last built.
   - `failure`: "N of M entries failed", or the build list couldn't be read.
   - `error`: "stopped by the operator".

### Repair and control

- **Build now** (per repository, in the UI) starts a build event for the current default-branch head immediately. It skips both the last-built check and the probe, so publishing always runs. This is the repair path: the probe only looks at an entry's own top-level path, so a probe-first run can't repair a broken closure. It is also how an operator who does not want to wait for the next poll gets a build — or a missed push — published.
- **Stop** cancels the running job, records it as stopped, starts no further jobs of that event, and reports `error`. A stopped event is over, not partly built.
- **Job timeout**: each job has one. Together with Stop, it bounds a hung build, which would otherwise hold up that repository's later commits.
- **Restart**: events in memory are lost, and a job that was running shows as interrupted, with its commit not recorded. The last-built record survives a restart, so nothing is rebuilt; a head that was never recorded is found by the poll that runs at start.

### Transcripts

Each job keeps its full transcript: the commands run and their output, stored verbatim with no per-job size cap. The job page shows it, and a plain-text version is available.

### Manual cache repair

- Broken narinfo: delete it, then Build now.
- Broken nar: delete its narinfo and the nar. Deleting only the nar changes nothing, because nix treats a path as valid once its narinfo exists and never rechecks nars.
- Broken dependency object: the probe still hits, so Build now. Publishing re-uploads exactly the members that became invalid.
- An interrupted publish can leave a false hit (top-level narinfo present, closure members missing). Fix it with the same recipe.
- Any hand-run nix command used in repair must bypass nix's local narinfo cache, or it reports deleted objects as still present.

## UI

The decided look is Clearlooks (GNOME 2 era) chrome as structure, not costume: one hand-written stylesheet, no CSS framework, no JavaScript required, light only. The full spec, mock, and UI acceptance criteria are in [docs/design/freezer-ui-clearlooks.md](docs/design/freezer-ui-clearlooks.md).

## Development

Run `devenv test` with the host Nix daemon available (Nix 2.34 is tested). It starts a loopback Silo S3 service, runs the end-to-end tests, and records current results in `.devenv/acceptance.log`. Tests use throwaway Git repositories, a locked static BusyBox fixture input, fresh signing keys and scratch Nix stores. The test bucket is cleared before each test. Do not point this environment at a production bucket.

`service.go` owns production scheduling. `loop.go` expands entries and executes individual jobs. The sequential `RunLoop` helper lives in `loop_test.go` for acceptance tests only.

Repository build lists use one YAML document with an `entries` list:

```yaml
entries:
  - packages.x86_64-linux.hello
  - legacyPackages.x86_64-linux.my-tools-*
```

Only the final attribute component may contain a glob (`*`, `?`, or character classes). Quoted Nix attribute names are accepted; output selectors (`^dev`, `^*`) are not. Unknown YAML fields and invalid entries reject the build list before Nix runs. `glob_cap` defaults to 200 total glob matches per event; a pattern that would exceed the remaining allowance fails entirely. `job_timeout` defaults to `30m` and includes evaluation, probing and publication. The signing key is passed by filename to Nix and is never read by Freezer.

Nix invocations separate options from paths with `--` and preserve evaluated path bytes. A probe is a hit only when Nix succeeds and reports that exact path; option-like values or extra whitespace cannot masquerade as a different published path. Static HTTP assets are limited to the stylesheet; templates are rendered only by the application.

## Running the service

The deployment route is one container under rootless Podman:

```sh
podman build -t freezer:0.1.0 .
```

The image carries Nix 2.34.8, git and curl, so the host needs no Nix of its own, and the service drives the Nix inside the container. `deploy/` holds the compose file that pins the deployment (image tag, port, mounts, environment, health check, restart policy and the container capabilities Nix's sandbox needs) plus example configuration and environment files; [the runbook](docs/runbook.md#deploy-as-a-container) covers the image, secrets, the compose project, R2, public-domain substitution, self-hosting, retention and repair.

Running the binary directly is also supported: `devenv shell -- go build -o .devenv/freezer .`, or `nix build` and then `./result/bin/freezer /path/to/server.yaml` (`packages.<system>.default`). That route uses the host's Nix, so the host's daemon must be at least 2.33 (2.34 tested); use at least 2.33.6 for the backported path-addressing option when selecting the 2.33 series. The binary has one mode and takes only the configuration filename. Bind to localhost behind an authenticated reverse proxy or SSH tunnel; nothing needs to be reachable from outside, because triggering is polling.

```yaml
# Container deployments must listen on 0.0.0.0; the binary route binds loopback.
listen: 0.0.0.0:8080
state_dir: /var/lib/freezer
global_build_cap: 2
glob_cap: 200
job_timeout: 30m
retention: 1000
# Required and positive: polling is the only trigger. 10m to 1h is the usual
# range; a poll costs one branch resolution per repository.
poll_interval: 15m
public_url: https://freezer.example.com
cache:
  bucket: public-nix-cache
  endpoint: https://ACCOUNT_ID.r2.cloudflarestorage.com
  signing_key: /etc/freezer/signing.key
repositories:
  - name: personal-flake
    url: https://github.com/OWNER/REPOSITORY.git
    github: OWNER/REPOSITORY
```

Set `GITHUB_TOKEN` (or the private config's `github_token`) to a shared token allowed to create commit statuses. Nix reads `AWS_ACCESS_KEY_ID` and `AWS_SECRET_ACCESS_KEY` from the environment. Keep the server config private: it names the repositories you publish for and may carry the GitHub token. Register public repositories only; all published outputs are public. `github_api_base` defaults to `https://api.github.com` and exists as the network test seam.

State lives in `state_dir/freezer.sqlite`; persistent checkouts live under `state_dir/checkouts/`. Restarting loses queued events and marks running jobs interrupted, retaining the transcript already recorded. Build now and Stop are HTML form actions. Job history, paging and full plain-text transcripts require no JavaScript. The last-built commit survives a restart.

`retention` keeps the newest N jobs per repository (default 1000). It runs at startup and after each event; old job rows and their transcripts are deleted together. Running jobs are protected. Per-repository last-built state is independent of retained history. SQLite does not shrink the database file automatically; reclaiming space with `VACUUM` remains an admin operation.

## Documents

- [GLOSSARY.md](GLOSSARY.md): the vocabulary. Use these terms, and avoid the alternatives listed under each.
- [ROADMAP.md](ROADMAP.md): phases, acceptance lists, deferred work, and resolved questions.
- [docs/adr/](docs/adr/): decisions and why they were made.
- [docs/design/](docs/design/): the UI spec and its prototype.
- [docs/runbook.md](docs/runbook.md): deployment, cache verification and repair, backups, self-hosting, and the real-host acceptance record.
