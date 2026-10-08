# The signing key is a file, and nothing secret lives in the database

The ed25519 signing keypair lives in a file with mode 0600 whose path is in the server config, and nix signs with it through the store URL's `secret-key=` parameter. Nothing is encrypted inside SQLite.

The URL parameter is the mechanism, not a stylistic preference: `nix copy` takes its signers from the *destination store's* settings. The global `secret-key-files` setting is not consulted by `nix copy` at all — pointing it at a nonexistent file changes nothing (verified). It signs paths when the *local store registers build outputs*, so on a host that sets it, a path arrives at the copy already signed. Both routes yield a signed narinfo; only the URL parameter is under freezer's control.

Nix can only sign from a file, so a key that existed solely inside a sealed database column would have to be decrypted and written to disk for each copy operation regardless — buying a shorter on-disk window in exchange for decryption, permission, cleanup, and crash-leftover handling. Keeping it in a file also keeps the database from being a credential store: copying the database no longer copies the cache's trust anchor.

Freezer itself never reads the private key.

## Consequences

- The key file must be backed up separately from the database, and it is the thing that must be protected; the database dump is not sensitive.
- Losing the key file means generating a new keypair and updating every consumer's `trusted-public-keys`; the cache's existing objects remain valid but nothing new is trusted until consumers update.
- The public half comes from `nix-store --generate-binary-cache-key` at creation time. No in-tool key generation is needed.
