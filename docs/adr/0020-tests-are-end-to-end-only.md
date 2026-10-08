# Tests are end-to-end only, against a real object store

There are no unit tests. Every test exercises real nix, real git, and a real object store, and asserts on observable results. Tests for the service phases additionally drive the built binary over HTTP; the v0 loop tests call the loop directly, because in v0 there is no service to drive — building one early would invent a surface the design does not have.

Substitutes are always **network stand-ins for real services**, never in-process fakes: the S3 service stands in for R2, and a local HTTP server stands in for `api.github.com` through the configurable API base (ADR 0021). Tests may use the object-store client (`mc`) for destructive setup — emptying the bucket, deleting one object to exercise the repair path — and for nothing else.

`devenv test` is the only supported way to run them: it brings up the object store from `devenv.nix` and then runs `go test ./...`. The store is a real S3 API on loopback with `region = "auto"` and path-style addressing, so the store URL the tests exercise is the same shape the production URL has — endpoint, region, addressing style, credentials, compression, signature, and multipart all take the real code path rather than a stand-in.

The service is **not** upstream MinIO: nixpkgs refuses `pkgs.minio` as insecure (six CVEs, upstream abandoned, with a migration note), so the dev environment runs the community fork, whose binary is still named `minio` and takes the same flags.

## Consequences

- The suite covers the loop as a whole — expand, `.outPath`, probe, copy, substitute — and the assertions are the ones that matter: a published path can be fetched back into a scratch store with its hash and signature verified, a second run copies nothing, a deleted narinfo is repaired, a deleted nar is not, and an entry publishes its default output only.
- **There is no fast feedback layer.** A mistake in entry parsing or store-URL assembly is found by running the whole loop against a live store, not by a one-second test. Accepted: those two pieces are small, and the failure they cause is loud.
- The suite needs a nix daemon and a store, so it cannot run just anywhere: `devenv test` is the contract, and CI would have to provide the same environment.
- What a test cannot reach is what is not local: the Cloudflare custom domain in front of the bucket, R2's own size ceilings, and GitHub itself. Those stay design-time facts (ADR 0003, ADR 0010) rather than tested behaviour, and the suite must not pretend otherwise.

## Considered Options

- **Unit tests for the pure parts** (entry parsing, pattern rules, store-URL assembly): rejected. They would be cheap and fast, and they would still not have caught the failures that actually mattered here — the false cache hit, the nar that is never re-checked, and the output selector that silently published the wrong output. All three were found by running the real loop and looking at what the store received.
- **A `file://` cache as the test double**: used while the design was being established, and rejected for the committed suite because it skips exactly the parts that carry risk in production — credentials, SigV4, addressing style, multipart, and the `endpoint` parameter itself.
