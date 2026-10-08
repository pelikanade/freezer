# freezer

A self-hosted build service for nix flakes that publishes built store paths to an S3-compatible binary cache, so consumers substitute them instead of rebuilding.

## Language

**Build server**:
The long-lived process that receives build events, builds jobs, and publishes results to the cache.
_Avoid_: builder, worker, daemon, CI

**Registered repository** (Repo):
A public git repository named in the server's configuration, which is the only kind that may be built. Repositories are never discovered implicitly, and private ones are not registered at all.
_Avoid_: project, source, target

**Ref policy**:
Which refs of a registered repository qualify for building.
_Avoid_: branch rules, trigger rules

**Build list**:
The flake output attribute paths a repository declares as buildable. It lives in the repository.
_Avoid_: allowlist, include list, job list

**Entry**:
One element of a build list: a flake output attribute path whose final component may be a glob. A glob names a pattern rather than a job.
_Avoid_: target, package, item

**Job**:
One entry built to completion, up to and including publishing its outputs. The unit of scheduling and reporting.
_Avoid_: task, build (a build is the nix operation, not the unit of work)

**Build event**:
A push to a qualifying ref, or a manual submission, from which jobs are created.
_Avoid_: trigger, run, pipeline

**Cache hit**:
A job whose outputs are already published, so no build is required.
_Avoid_: skip, up-to-date, warm

**Substituter**:
Nix's term for a store a client may fetch prebuilt paths from. Here, the published binary cache.
_Avoid_: mirror, remote, upstream

**Binary cache**:
The S3-compatible bucket holding the narinfo and nar objects that make built outputs substitutable.
_Avoid_: store, bucket

**Signing key**:
The ed25519 keypair whose public half consumers trust. Narinfos published under it are what make the cache trustworthy.
_Avoid_: secret key, certificate

**Narinfo**:
The per-store-path metadata object a substituter serves: hashes, references, size, signature.
_Avoid_: index, manifest, metadata

**Closure**:
A store path together with everything it references, transitively.
_Avoid_: dependencies, deps

**Commit status**:
The build result reported back to the forge for a built commit.
_Avoid_: check, report, notification

**Admin**:
The person who operates the build server: edits its configuration and reads jobs, logs, and results. Owns no build lists, which live in repositories.
_Avoid_: user, operator, owner
