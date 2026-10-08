# One bucket, one flat namespace, one signing key

Every registered repository publishes into the same S3 bucket, at its root — no per-repository prefix — and every narinfo is signed by a single server-wide ed25519 keypair. All artifacts of all repositories therefore sit behind **one** substituter URL (`https://cache.example.com`) and **one** `trusted-public-keys` entry.

This is what personal use buys: consumer configuration is a single line, the server's cache target is a single bucket, and there is no namespace mapping to invent or to keep in sync with repository names. Store paths are content-addressed, so two repositories that build the same derivation dedupe onto the same objects rather than colliding — a shared namespace is a benefit here, not a hazard.

## Considered Options

- **A namespace per repository** (`s3://bucket/<owner>/<repo>`, reachable because nix treats a URL path component as a key prefix since 2.33). Rejected: it buys isolation nobody needs at this scale, and it rests on behaviour that is **undocumented upstream**. Dropping it removes the design's only dependence on unspecified nix behaviour.
- **A bucket per repository**: rejected — N buckets, N custom domains, N consumer configurations.

## Consequences

- **Revocation is all-or-nothing.** Retiring one repository's artifacts is not a scoped operation: everything is at the bucket root, so there is no prefix to delete. Accepted, and the same was already true of the single shared key.
- Nothing in the cache records which repository published an object. Freezer writes only what nix writes for it, and keeps no index.
- Key rotation means trusting two public keys for a while. Narinfo carries repeated `Sig:` lines, so that works without republishing anything.
- An empty bucket needs exactly one publish before it is usable: nix writes `nix-cache-info` at the root on the first copy.
