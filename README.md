# Freezer

A self-hosted, webhook-driven build service for nix flakes. It publishes built store paths to a public, signed, S3-compatible binary cache (Cloudflare R2), so consumers substitute them instead of rebuilding.

Status: implementing the rewrite. The real Nix/S3 loop now publishes exact entries and verifies substitution, hashes and signatures against a scratch store. The service and UI are still being implemented; progress and acceptance are in [ROADMAP.md](ROADMAP.md).

## Problem

Keep a single maintainer's public flakes pre-built. When the default branch of a registered GitHub repository moves, build the flake outputs that repository declares and publish them, signed, to one public cache behind a custom domain. Consumers add one substituter URL and one trusted public key, and that covers every registered repository.

It is for one admin running it on their own host for their own public repositories. It is not CI: no PR or pre-merge builds, no other forges, no users or auth of its own, no metrics.

## Shape

**Product surfaces**

- **Webhook endpoint**, one per registered repository: `POST /hooks/<repo>`. GitHub calls it on push.
- **Web UI**, the only control surface: home (repositories and recent jobs), job page (details and transcript), and the Build now and Stop actions. There is no CLI. Anything the UI can't do is a config edit and a restart.
- **Server config file**, edited by the admin: registered repositories with their webhook secrets, cache target, signing key path, glob cap, global build cap, job timeout, retention, and GitHub token.
- **Build-list file** in each repository (`.freezer.yaml`): the repository's build list, and nothing else.

**External pieces**

- **GitHub**: push webhooks in, commit statuses out (one shared service token).
- **The host's nix daemon** (≥ 2.33): does all building, publishing, and cache probing.
- **The S3-compatible bucket** (R2), read publicly through a custom domain. Freezer never serves the cache itself.
- **A reverse proxy** (or an SSH tunnel to a localhost bind) in front of the UI. It provides TLS and access control.

Terms are defined in [GLOSSARY.md](GLOSSARY.md). The reasons for each choice are in [docs/adr/](docs/adr/).

## Behavior

### Push, build, publish

1. GitHub POSTs a push to the repository's webhook endpoint. Unregistered repositories are rejected. The signature is checked against that repository's own secret, and a bad signature produces no job at all. Pushes that don't touch the default branch are acknowledged and dropped. Force-pushes count.
2. The event goes into the repository's queue, which holds one waiting event. A newer event replaces it, and a running event is never cancelled by a push. One event runs per repository at a time, and different repositories run in parallel. The webhook always accepts, since there is no backlog to fill. A superseded event leaves no trace and gets no commit status.
3. When the event starts, the default branch head is resolved then, not when the push arrived, so a commit that a faster push already overtook is never built. If the resolved commit equals the last-built commit, the event ends there with no jobs.
4. Freezer posts a `pending` commit status on the resolved commit (not the payload's commit, which can differ after a force-push). It reads the repository's build-list file without evaluating any nix. A missing or unreadable file means `failure` and nothing gets built: reading config must never build anything.
5. Each entry becomes jobs:
   - **Glob expansion**: a glob entry lists the attribute names under its declared prefix, one level only, capped per event. Over the cap, every entry of that pattern fails with a clear message and none build; truncating silently would make the build list a lie. A prefix that doesn't exist fails that pattern's entries only, and other entries still build. A pattern that matches nothing is not an error, and its job is labelled *matched nothing*.
   - **Output path**: evaluate the entry's output path without building. Evaluation is never allowed to build (import-from-derivation is off here). A failed evaluation is not a job failure. It just counts as a miss.
   - **Probe**: ask the cache whether that path is published. Only a definite "published" answer is a cache hit, and the job stops there. Every other outcome is a miss. The probe may be wrong toward a miss (costing a rebuild), never toward a hit.
   - **Publish on a miss**: build if needed and copy the path to the cache, signed. This step decides whether an entry is buildable, so its error is the one the job reports. Import-from-derivation stays at the host default here, so an entry that needs it to evaluate still builds.
6. Final commit status, one per event:
   - `success`: every job succeeded, including when every entry was a cache hit (the commit is verified built even though nothing was published). The commit is recorded as last built.
   - `failure`: "N of M entries failed", or the build list couldn't be read.
   - `error`: "stopped by the operator".

### Repair and control

- **Build now** (per repository, in the UI) starts a build event for the current default-branch head. It skips both the last-built check and the probe, so publishing always runs. This is the repair path: the probe only looks at an entry's own top-level path, so a probe-first run can't repair a broken closure. It is also how a missed push gets built.
- **Stop** cancels the running job, records it as stopped, starts no further jobs of that event, and reports `error`. A stopped event is over, not partly built.
- **Job timeout**: each job has one. Together with Stop, it bounds a hung build, which would otherwise hold up that repository's later commits.
- **Restart**: events in memory are lost, and a job that was running shows as interrupted, with its commit not recorded. The last-built record survives a restart, so nothing is rebuilt.

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

## Documents

- [GLOSSARY.md](GLOSSARY.md): the vocabulary. Use these terms, and avoid the alternatives listed under each.
- [ROADMAP.md](ROADMAP.md): phases, acceptance lists, deferred work, and resolved questions.
- [docs/adr/](docs/adr/): decisions and why they were made.
- [docs/design/](docs/design/): the UI spec and its prototype.
