# The cache is push-only and publicly readable

The server never serves the cache. It publishes to an S3-compatible bucket (Cloudflare R2) exposed read-only through a custom domain; consumers configure that URL as a substituter plus the signing public key as trusted.

The rejected alternative — a read-through serving endpoint in the same binary — would add auth, availability responsibility, and a second deployment surface to a process whose job is to build. It buys nothing while the cache is public.

## Consequences

- **Everything published is world-readable**, so only public repositories belong in the registry. Freezer does not verify visibility: that would mean calling GitHub to second-guess a deliberate config edit, which is precisely the machinery ADR 0008 declines to build. The rule lives where the operator meets it — a comment in the config file, and this document — and a private repository registered anyway has its artifacts published, permanently and publicly.
- The server's own cache-hit probes never touch the CDN — they reach the S3 API through nix — so neither a client's one-hour negative cache of narinfo lookups nor a CDN configured to cache everything can hide a freshly published path from the skip logic. (Cloudflare's default rules are reported not to cache extension-less objects such as `<hash>.narinfo` or `nix-cache-info`; unverified, and not load-bearing, since the probe bypasses the CDN either way.)
- Consumers need one substituter URL and one trusted key, covering every registered repository (ADR 0005).
