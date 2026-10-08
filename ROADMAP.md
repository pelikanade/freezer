# Roadmap

Three phases, sized by scope and not by time. Each phase ends when its acceptance list is demonstrated — v0 and v1 by the end-to-end suite (`devenv test`), v2 on the real host with the results recorded in the runbook (itself a v2 deliverable). Acceptance is written first as a failing test (ADR 0020: end-to-end only, no unit tests).

The loop written in v0 is not scaffolding: those functions become the job runner the service calls in v1.

## v0 — the loop is real

**Scope.** The whole job loop: expand (glob or exact entry), `.outPath`, probe, `copy`. Plus the configuration struct and its YAML loading, entry parsing, per-entry results and log capture, and a per-job timeout.

**Not in v0.** HTTP, database, UI, webhook, commit status, scheduling, cancellation. Nothing is persisted — v0 is driven by tests, not operated.

**Acceptance** — one end-to-end test per assertion, calling the loop directly (there is no service to drive yet), against the devenv S3 service and a throwaway git repository:

1. An exact-attribute entry publishes, and the probe then hits.
2. The published path is fetched back into a scratch store with its hash and signature verified.
3. A second run of the same entry reports a cache hit and publishes nothing.
4. A glob entry expands to N entries, all of which are published.
5. A pattern matching more than the per-event cap fails entirely and builds none of its entries.
6. A pattern whose prefix does not exist fails, while other entries in the same build list still build.
7. An attribute that is not a derivation: the evaluation failure is not a job failure; the copy step reports the real error.
8. An attribute that needs import-from-derivation: evaluation fails with IFD disabled, and the copy step then builds and publishes it.
9. A multi-output attribute publishes its default output only; the other output probes as a miss.
10. Deleting the entry's narinfo is repaired by a rerun; deleting only the nar repairs nothing.

**Plumbing.** The fixture repository lives in `testdata/`: a flake plus `.freezer.yaml`, with variants for multi-output, IFD and glob entries. Each test empties the bucket before it starts, using `mc` for that destructive setup step and nothing else. The store URL the tests use is the production shape: flat, `region=auto`, path addressing, credentials from the environment. Every nix invocation in a test disables nix's local narinfo cache (`--option narinfo-cache-positive-ttl 0 --option narinfo-cache-negative-ttl 0`): with nix's defaults — 30 days for a positive answer, one hour for a negative one — a probe answers from that cache, so a bucket that was just emptied still looks populated and these assertions would pass for the wrong reason.

## v1 — the service exists

**Scope.** The configuration file, the HTTP server, the webhook with HMAC verification and default-branch resolution, SQLite state through GORM, commit statuses, scheduling (one event per repository, coalescing, the global cap), the job timeout and manual cancel, and the read-only UI with build now and stop. The UI is not optional here: the CLI does not exist, so it is the only control surface.

**Acceptance** — driving the built binary over HTTP:

1. A correctly signed push triggers a job whose outputs are substitutable from the cache.
2. Pushing the same commit again does not build it again.
3. Two rapid pushes coalesce into one build.
4. A push to a non-default branch is ignored.
5. A push with a bad signature produces no job at all.
6. An event arriving during a build of the same repository queues behind it rather than running concurrently.
7. Commit statuses go pending then success, carrying the failed-entry count.
8. An event where every entry is a cache hit still reports success.
9. Build now over HTTP skips the probe and re-runs the copy.
10. Stop aborts a running job before its timeout.
11. A restart preserves the last-built record, so nothing is rebuilt.

**Doubles.** Two, both network stand-ins for real services rather than in-process fakes: the S3 service for R2, and a local `httptest` server for `api.github.com`, reached through the configurable API base (ADR 0021).

## v2 — in the real environment

**Scope.** The flake package and the NixOS module, the real R2 bucket and its custom domain, retention, the repair runbook, and self-hosting.

**Acceptance** — performed on the maintainer's host and recorded in the runbook rather than automated, because the real environment is what this phase is for:

1. After the module's configuration is applied, the service runs with the right user, state directory, key path and environment.
2. Publishing to the real R2 bucket, and substituting from it through the public custom domain, both work.
3. Freezer builds its own flake outputs through itself, and a second run is a cache hit.
4. Retention actually deletes old jobs and their logs.
5. The repair recipe is executed once for real: a broken object is deleted, build now is pressed, and substitution succeeds afterwards.

## Deferred — not in any of the three phases

`^output` selectors and `^*`; publishing `.drv` files; content-addressed derivations and realisations; metrics; automatic retries; store garbage collection; private repositories; pull requests and forks; forges other than GitHub; cache purge or repair tooling; users, roles and authentication.

Each of these is either recorded as a limitation in an ADR or a decision not to build something; none of them is half-built and waiting.
