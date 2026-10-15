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
Which refs of a registered repository qualify for building: the default branch only.
_Avoid_: branch rules, trigger rules

**Build list**:
The flake output attribute paths a repository declares as buildable. It lives in the repository.
_Avoid_: allowlist, include list, job list

**Entry**:
One element of a build list: a flake output attribute path whose final component may be a glob. A glob names a pattern rather than a job.
_Avoid_: target, package, item

**Matched nothing**:
A glob entry whose pattern matches no attributes. Not an error: its job is recorded as succeeded and labelled matched nothing.
_Avoid_: empty, no-op

**Job**:
One entry built to completion, up to and including publishing its outputs. The unit of scheduling and reporting.
_Avoid_: task, build (a build is the nix operation, not the unit of work)

**Transcript**:
The full, verbatim record of the commands a job ran and their output.
_Avoid_: log, logs

**Stopped**:
A job ended by an operator's Stop. Not a failure: its event reports error.
_Avoid_: cancelled, aborted

**Interrupted**:
A job that was running when the server restarted, so its commit is not recorded. It is not a failure.
_Avoid_: crashed, lost

**Build event**:
A push to a qualifying ref, a change of the default-branch head found by polling, or a manual Build now, from which jobs are created.
_Avoid_: trigger, run, pipeline

**Poll interval**:
How often the build server compares each registered repository's default-branch head with the commit last built, for deployments GitHub cannot reach. Off by default; the head check itself is the last-built check that pushes use.
_Avoid_: schedule, cron, heartbeat

**Build now**:
The manual action that starts a build event for a repository's current default-branch head. It skips the last-built check and the probe.
_Avoid_: rebuild, retry, re-run

**Queued event**:
The one build event waiting for its repository's current run to finish. A newer event replaces it, and a replaced event leaves no trace.
_Avoid_: backlog, pending

**Cache hit**:
A job whose outputs are already published, so no build is required.
_Avoid_: skip, up-to-date, warm

**Probe**:
The question, asked of the binary cache before any build, of whether an entry's output path is already published. Only a definite yes is a cache hit; every other answer is a miss.
_Avoid_: check, lookup

**Publish**:
Building an entry if needed, then copying its output path to the binary cache, signed.
_Avoid_: push, upload, deploy

**Last-built commit**:
The commit a repository last built successfully, so a redelivered webhook does not build it again. It is not a claim about what the cache holds.
_Avoid_: last build, head

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
The build result reported back to GitHub for a built commit.
_Avoid_: check, report, notification

**Admin**:
The person who operates the build server: edits its configuration and reads jobs, transcripts, and results. Owns no build lists, which live in repositories.
_Avoid_: user, operator, owner
