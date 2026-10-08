# Publication is trusted; failures surface late

Publication is no longer ours to verify: nix does the uploading (ADR 0010), so objects are checked as far as nix checks them and no further, and freezer never reads a bucket object back. What remains is the same posture applied to everything else — no preflight of the host's nix configuration at startup, and no in-tool repair or purge of published objects. The server trusts its host and does not build machinery to anticipate its own mistakes.

Each individually is defensible. Together they define where failures are noticed:

- A published object that is wrong in a way nix did not catch is first discovered by a *consumer* whose substitution fails; nothing on the server side ever reads an object back.
- The repair path is manual, and its exact shape is now verified rather than inferred. To nix, a path is valid as soon as its **narinfo** exists — nars are never re-checked. So a broken *narinfo* is repaired by deleting that object and pressing "build now"; a broken *nar* is repaired by deleting its narinfo **and** the nar, because deleting the nar alone changes nothing (`nix copy` reports `copying 0 paths`). And because the probe only inspects the entry's own top-level path, deleting a *dependency's* object leaves the probe reporting a hit — which is why "build now" must skip the probe and run the copy, after which the copy re-uploads exactly the members that stopped being valid (verified: `copying 1 paths`). One trap belongs in the runbook: nix's own narinfo cache answers for 30 days, so a *hand-run* `nix copy` or `nix path-info` will insist that a just-deleted object is still there — any manual repair command must pass `--option narinfo-cache-positive-ttl 0 --option narinfo-cache-negative-ttl 0`, which freezer's own invocations always do.
- Host misconfiguration (the service user outside `trusted-users`, sandbox disabled) appears as per-job failures carrying nix's own error text, with no preflight panel in the admin surface to explain it.
- The builder does not read from its own cache either. The host's configured substituters are the only sources for build-time dependencies.

## Considered Options

- Verifying published objects after each upload: rejected. With nix doing the upload there is no upload of ours to verify, and the only meaningful check left is an end-to-end substitution by a real client — which belongs in testing, not in every publish.
- A preflight check of the host's nix settings: rejected in favour of letting nix report its own errors per job.
