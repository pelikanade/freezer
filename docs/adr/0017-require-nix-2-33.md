# Require nix 2.33 or newer (2.34 tested)

Everything this design leans on arrived with nix's HTTP-based S3 backend rewrite in 2.33:

| Capability | Minimum |
|---|---|
| zstd nar reading and writing | 2.4 |
| `endpoint`, `region`, custom-endpoint addressing, `multipart-*` | 2.33.0 |
| `addressing-style` | 2.34.0 (backported to 2.33.6) |
| no in-memory buffering of large objects | 2.33.0 |

Below 2.33 there is no `endpoint` parameter at all — the older S3 backend speaks only to AWS — so R2 is not addressable; there is no multipart setting to reach for either, and whole objects were buffered in memory (large downloads were known to fail outright). That is the kind of floor worth stating, rather than discovering as a confusing error. 2.33 is the minimum that can work; 2.34 is the version to develop and test against (2.34.8 is what the design was verified on).

## Consequences

- The floor is stated in the README and satisfied by construction in the container image, which pins Nix 2.34.8, and the implementation's first test is publish-and-substitute against an empty bucket: it exercises the whole URL-parameter story — endpoint, region, addressing, compression, signing, multipart — against whatever Nix the deployment actually runs.
- **Nothing validates the version at startup** (ADR 0008): a host on an older nix produces nix's own confusing error rather than a clear "your nix is too old".
- The floor is a property of the Nix that runs Freezer, not of the flake: pinning nixpkgs in freezer's own flake pins the package that is built, not the toolchain that publishes. The container image is where the two arrive together.

## Considered Options

- **Supporting nix ≤ 2.32**: rejected. The older backend cannot address a custom endpoint at all, buffers whole objects in memory, and has no multipart support, so publishing to R2 would fail in several distinct ways.
- **Requiring 2.35** for per-store retry parameters and parallel zstd compression: rejected as a floor nobody can meet yet, for improvements this workload does not need.
