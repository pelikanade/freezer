# Publication and cache probing both belong to nix

Freezer contains no S3 client and no cache protocol code. A job's entire body is nix invocations:

1. `nix eval --json "<flake>#<prefix>" --apply builtins.attrNames` — only when the entry is a glob, and only for the prefix the entry declares (ADR 0018).
2. `nix eval --raw "<flake>#<attr>.outPath"` — the output path, without building it. Both evaluation steps pass `--option allow-import-from-derivation false`, because with import-from-derivation enabled this step *can* build as a side effect, which would make a "probe" cost a build (verified: with the option off, an attribute that needs it fails with `cannot build '…drv^out' during evaluation because the option 'allow-import-from-derivation' is disabled`).
3. `nix path-info --store "<cache URL>" <outpath>` — exit 0 means it is published, so the job is a cache hit and stops here.
4. `nix copy --to "<cache URL>" "<flake>#<attr>"` — builds if needed, then writes the nar, narinfo, signature, and `nix-cache-info` at the bucket root. Here import-from-derivation is deliberately left at the host's default: building is this step's job, so an entry that only needs IFD to *evaluate* still builds rather than being rejected.

**Only step 3's exit 0 is a hit; every other outcome is a miss.** That includes a failing step 2 (a content-addressed derivation, a fixed-output path whose hash is not yet known, an attribute that is not a derivation at all), `path … is not valid`, and `'<url>' does not appear to be a binary cache` on a bucket that has never been published to. Two consequences are deliberate:

- A failing step 2 is **not** a job failure. `nix copy` is the arbiter of whether an entry is buildable, so an entry whose output path cannot be computed still gets its chance there — which is how "content-addressed entries always build" is actually produced, rather than inferred. If it is not buildable, the failure arrives with the copy step's own error message, which is usually the informative one.
- The probe is allowed to be wrong in the miss direction, which costs a rebuild. It is never allowed to be wrong in the hit direction.

"Build now" is this same loop with step 3 removed. It has to be, because the probe only ever looks at the entry's own top-level path: a probe-first run cannot repair anything.

Everything the cache needs travels as store-URL parameters rather than host configuration: `endpoint=`, `region=auto`, `addressing-style=`, `compression=zstd`, `secret-key=<path>`, `multipart-upload=true`. Nix's defaults are otherwise wrong for us and invisible to a preflight check — NAR compression is xz, multipart upload is **off** (100 MiB threshold, 5 MiB chunks) — so freezer sets what it needs in the URL it constructs, and touches no nix configuration.

## Considered Options

- **A hand-rolled writer**: gave us control of compression, parallelism, and the skip decision, at the cost of owning narinfo grammar, the reference scanner, ed25519 signing, multipart uploads, and R2's size ceilings. Rejected: a bug there poisons a cache that consumers trust, and detecting it requires exactly the client-side test nix already ships.
- **Our own probe** (signed HEADs, or plain HEADs against the public URL): rejected once delegation demonstrably worked. A probe's failure mode is safe — misses cause rebuilds — and delegating keeps exactly one opinion in the system about where the cache is and how to talk to it.

## Consequences

Behaviour verified against nix 2.34.8 (2026-10): first with a local `file://` cache standing in for the bucket, then — for the store URL, credentials, signature, compression and substitution path — against a real S3 API on loopback provided by the dev environment (ADR 0020). The loop was run end to end over the S3 API: publish, probe for a hit, re-publish (`copying 0 paths`), and fetch the path back into a scratch store with its hash and signature verified against the generated key.

- **The S3 path is real.** `endpoint=`, `region=auto`, `addressing-style=path`, credentials from the environment, `compression=zstd`, `secret-key=` and `multipart-upload=true` were exercised against an S3 API, not a `file://` directory: the published narinfo and nar were then fetched back and verified, so the URL freezer constructs is known to work against the same interface R2 speaks.
- **An entry publishes its default output only.** `nix copy` of a two-output derivation published one path, `out`; the `dev` output was never uploaded, and `nix eval --json` on the attribute returns only the `out` path, so nothing in the loop can even see the others. Nix's output selectors do work — `…#attr^dev` published `dev` alone and `…#attr^*` published both — but entries in v1 do not carry them: one entry means one output means one probe path, and the entries stay the simple thing ADR 0001 describes. Extra outputs are therefore a documented limitation rather than a feature with a workaround.
- **Evaluation can build unless it is forbidden**, and the loop forbids it for both evaluation steps (see step 2): otherwise a probe could build, and a jump that only exists to save work would start doing the work.
- **A cache hit says nothing about the closure.** With a dependency's narinfo deleted, the probe on the entry's top-level path still exits 0. `nix copy` then re-uploads exactly the member that stopped being valid (`copying 1 paths`) — but only if something lets it run.
- **Nars are never re-checked.** With every narinfo present and every nar deleted, both the probe and `nix copy` report success (`copying 0 paths`) and nothing is restored.
- **An answer is only as fresh as nix's local narinfo cache.** Verified: with nix's defaults (`narinfo-cache-positive-ttl` 30 days, `narinfo-cache-negative-ttl` 1 hour) a probe on an *emptied* bucket still reported the path as published. It is not only the probe: with a warm cache, `nix copy` against a bucket it had just been seen emptied printed `copying 0 paths` and restored nothing. Freezer therefore passes `--option narinfo-cache-positive-ttl 0 --option narinfo-cache-negative-ttl 0` to **every** cache-facing invocation, probe and copy alike, so an answer always describes the bucket rather than a month-old opinion of it. Without that, a deleted object stays invisible to the skip logic for as long as the cache lives, and ADR 0008's repair recipe would appear to do nothing at all.
- An interrupted publish can therefore leave a false cache hit — the top-level narinfo written, closure members missing. Its repair is ADR 0008's recipe, not a retrigger.
- Probing an empty bucket initializes it: nix writes `nix-cache-info` as part of opening the store, so a read-shaped probe can create an object. It still reports a miss, which is the behaviour the skip logic needs.
- The probe is nix *parsing* the narinfo rather than our own HEAD request, so it cannot be fooled by an object that answers 200 with a corrupt body.
- Signatures accumulate. A path that already carries another cache's signature is republished with that signature intact next to ours — which is also what makes key rotation a matter of trusting two keys for a while.
- Requires nix 2.33 or newer, with 2.34 the tested version (ADR 0017). Below that there is no `endpoint` parameter at all, so R2 cannot be addressed.
- Freezer needs no S3 credentials of its own: the same URL carries them through nix's environment.
- A NAR above the store's single-PUT ceiling (R2: 4.995 GiB) uploads only with `multipart-upload=true`. Without it nix refuses any nar over 5 GiB outright (`file too large for S3 upload without multipart`), which makes that setting a correctness requirement rather than tuning.
