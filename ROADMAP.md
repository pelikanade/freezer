# Roadmap

Three phases, sized by scope and not by time. Each phase ends when its acceptance list is demonstrated — v0 and v1 by the end-to-end suite (`devenv test`), v2 on the real host with the results recorded in the runbook (itself a v2 deliverable). Acceptance is written first as a failing test (ADR 0020: end-to-end only, no unit tests).

The loop written in v0 is not scaffolding: those functions become the job runner the service calls in v1.

## Implementation evidence

- v0 complete: `devenv test` passed on 2026-10-10 (11 tests, 32.200 s). All ten v0 acceptance items run against real Nix/Git/S3, plus matched-nothing and timeout behavior. The exact-entry test loads both YAML files. Current output: `.devenv/acceptance.log`.
- v1 service complete: all eleven HTTP acceptance items passed on 2026-10-10, plus interrupted-transcript recovery and missing-build-list rejection (24 tests total, 64.478 s). Actual browser smoke exercised Build now against the built binary and S3, producing succeeded, failed and matched-nothing jobs.
- v1 UI complete: all seventeen UI acceptance items exercised in Chromium at 1280×800 and 390×844, including no-JavaScript actions, paging, state-specific views, keyboard focus and measured contrast. Full regression: 27 tests passed in 80.316 s. Local screenshots and the item-by-item record live in `.project-map/ui-evidence.json`.
- v2 retention implemented: HTTP acceptance passed (5.238 s), and an actual service restart with `retention: 2` removed job 59 and its transcript from a 61-job fixture while jobs 60–61 remained readable. This is local evidence, not real-host v2 acceptance.
- v2 packaging implemented: `nix build .#default` built a static x86-64 executable; the installed package served HTTP and published an output whose S3 contents/signature verified. `nix flake check --all-systems` passed; aarch64 was evaluated, not built. [The runbook](docs/runbook.md) and the repository's own `.freezer.yaml` are present. The NixOS module that this line used to name was removed in favour of the container image ([ADR 0022](docs/adr/0022-deployment-is-one-container.md)).
- v2 container deployment implemented: the `Dockerfile` compiles the Go package with the official Go image and runs it on the official Nix image, and `deploy/compose.yaml` pins the whole deployment (image tag, loopback port, mounts, environment, health check, restart policy, and the container capabilities the Nix sandbox needs). The Quadlet unit the deployment started with was removed in favour of compose (ADR 0022, amended). Verified in this session against the base image's extracted root filesystem and against a runtime-stage image built by Podman, because the session could not create rootless-Podman user namespaces and could not unpack a Debian-based build image: the service served its UI, accepted a signed push, built, published to a loopback S3 service over the production store URL and substituted the result into a fresh store with `--sigs-needed 1`. Both the full `podman build` and the running service were verified later in the session, on this host, after the operator lifted the sandbox restrictions: the container built, started under compose, posted a `pending` commit status to the real repository and ran sandboxed builds. Real R2 publish and port publishing through a proxy remain for real-host acceptance.
- Triggering is polling-only ([ADR 0024](docs/adr/0024-triggering-is-polling-only.md), superseding ADR 0013 and absorbing ADR 0023): the webhook route, the per-repository secret, the HMAC check and the push-payload parsing are deleted, and `poll_interval` is required and positive. Each registered repository's default-branch head is resolved at start and on every interval afterwards, and a head that is not the commit last built starts the same event a push would have. The service now needs no inbound route at all, so a deployment can stay entirely private. Acceptance: the service tests trigger by starting the service, using a one-second interval where a second automatic event is needed — a polled head publishes and its statuses name the resolved commit, an unchanged head produces no second job or status, ticks coalesce behind a running event, a waiting Build now is not displaced (re-run with the rule removed, the queued job turned into a cache hit and the test failed), and the index renders while an event is queued (which caught the queued row still reading the removed payload fields). Local suite: 29 tests, 21 passed, 8 failed; every failure has one cause — this host's Nix trusts only `root @wheel`, so the suite's `--option narinfo-cache-*-ttl` is ignored with a warning and the test helpers, which merge stdout and stderr, parse the warning. The seven loop tests fail identically at the base commit, and the service test that verifies a published path by substituting it fails the same way. The service keeps the streams apart, so its own probe is unaffected.
- Deployment review: real-Nix regressions reproduced and fixed false cache hits from option-like outputs and whitespace normalization. The final package passed actual HTTP scenarios for both. Final local suite: 31 tests passed in 86.523 s; `go vet ./...` passed. Retention also verifies kept transcripts remain byte-for-byte unchanged.
- Local self-hosting verified against the final implementation: the packaged service compiled and published a temporary clone (one comment forced a fresh derivation), then a documentation-only commit produced a cache-hit job with no copy. Fresh-store substitution and hash/signature verification passed. Transcripts and fixture revisions are recorded in `.project-map/self-hosting-evidence.json`; this does not replace real-host acceptance.
- Remaining: all five real-host acceptance items. No deployment host, R2 credentials or public custom domain are configured in this session; their acceptance record is explicitly waiting in the runbook.

## v0 — the loop is real

**Scope.** The whole job loop: expand (glob or exact entry), `.outPath`, probe, `copy`. Plus the configuration struct and its YAML loading, entry parsing, per-entry results and transcript capture, and a per-job timeout.

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

**Scope.** The configuration file, the HTTP server, the push trigger with its HMAC verification and default-branch resolution, SQLite state through GORM, commit statuses, scheduling (one event per repository, coalescing, the global cap), the job timeout and Stop, and the UI with Build now and Stop. (The webhook trigger was later removed in favour of polling it already fed through: ADR 0024.) The UI is not optional here: the CLI does not exist, so it is the only control surface.

**Acceptance** — driving the built binary over HTTP:

1. A correctly signed push triggers a job whose outputs are substitutable from the cache.
2. Pushing the same commit again does not build it again.
3. Two rapid pushes coalesce into one build.
4. A push to a non-default branch is ignored.
5. A push with a bad signature produces no job at all.
6. An event arriving during a build of the same repository queues behind it rather than running concurrently.
7. Commit statuses go pending, then success or failure, carrying the failed-entry count.
8. An event where every entry is a cache hit still reports success.
9. Build now over HTTP skips the probe and re-runs the copy.
10. Stop aborts a running job before its timeout.
11. A restart preserves the last-built record, so nothing is rebuilt.

UI acceptance criteria are in [docs/design/freezer-ui-clearlooks.md](docs/design/freezer-ui-clearlooks.md), section 6.

**Doubles.** Two, both network stand-ins for real services rather than in-process fakes: the S3 service for R2, and a local `httptest` server for `api.github.com`, reached through the configurable API base (ADR 0021).

## v2 — in the real environment

**Scope.** The flake package and the container image, the real R2 bucket and its custom domain, retention, the repair runbook, and self-hosting.

**Acceptance** — performed on the maintainer's host and recorded in the runbook rather than automated, because the real environment is what this phase is for:

1. After the container is installed, the service runs with the right state directory, key path and environment.
2. Publishing to the real R2 bucket, and substituting from it through the public custom domain, both work.
3. Freezer builds its own flake outputs through itself, and a second run is a cache hit.
4. Retention actually deletes old jobs and their transcripts.
5. The repair recipe is executed once for real: a broken object is deleted, build now is pressed, and substitution succeeds afterwards.

## Deferred — not in any of the three phases

`^output` selectors and `^*`; publishing `.drv` files; content-addressed derivations and realisations; metrics; automatic retries; store garbage collection; private repositories; pull requests and forks; forges other than GitHub; cache purge or repair tooling; users, roles and authentication.

Each of these is either recorded as a limitation in an ADR or a decision not to build something; none of them is half-built and waiting.

## Resolved questions

These were open forks in the design. The owner confirmed ADR 0019 and ADR 0004 as written on 2026-10-10, so both answers stand for v1.

- **Global build cap.** Is there a server-wide cap on concurrent builds across repositories, or only per-repository serialization plus the per-event glob cap? ADR 0019 adds a global cap (config).
- **Concurrency inside an event.** Do one event's jobs build concurrently, or one after another? ADR 0019 answers: concurrently, bounded by the global cap.
- **Default branch source.** Is the default branch read from git (the remote's `HEAD`) or from the webhook payload? ADR 0004 answers: git, via `git ls-remote --symref`. The payload only showed whether a push touched that branch, and no payload exists now (ADR 0024).
