# Triggering is webhook-only; there is no reconcile sweep

A build starts when GitHub delivers a push webhook for a registered repository's default branch, which is resolved from the remote's `HEAD` symref through git (ADR 0004) after the signature is verified. There is no periodic comparison against the remote head, and no durable queue: events live in memory until they run (ADR 0019 settles what happens to events when several arrive for one repository).

Accepted consequence, stated plainly: GitHub does not automatically redeliver failed deliveries, and in-memory events die with the process. **A push that arrives while the server is down or restarting is never built and never reported, and nothing in the system notices.** Recovery is manual: press "build now" in the UI, or push to the branch again and let the webhook do it.

A periodic sweep — comparing each repository's remote head against the commit last built — would close that gap, and was considered and rejected: for a single-maintainer deployment the extra polling path and its traffic were judged not worth it against a recovery action that already exists. The same reasoning applies to a durable event queue, which cannot repair a webhook that was never delivered in the first place.

**Amended by [ADR 0023](0023-triggering-can-poll-the-branch-head.md):** that sweep now exists as `poll_interval`, off by default, for a deployment GitHub cannot reach. The reasoning above still holds for a deployment that has a publicly reachable webhook route.

## Consequences

- Silence is ambiguous: the absence of a commit status can mean no push happened, no webhook arrived, or the server was down.
- The recorded last-built commit is a de-duplication aid (do not rebuild the same commit), not a claim about what has been built; ADR 0019 defines when it is consulted.
- Each repository has one persistent checkout in the state directory: fetch the commit that was re-resolved, check it out detached, and never write to it (no lock-file updates, no commits). It is never cleaned up either, so it grows like the nix store does — the same accepted consequence as ADR 0015's missing garbage collection.
- The commit status is posted for the commit that was actually built — the re-resolved head, not the payload's `after`, which can differ after a force-push — as one status per event (pending when the event starts, then success or failure with the failed-entry count). An event where every entry is a cache hit still reports success: the commit has been verified built, even though nothing was published.
- The webhook secret is per repository, a field in the server config beside the repository's URL. One global secret would let any registered repository forge any other's events.
- Builds are fast to start after a push and, with webhooks only, there is no polling traffic against GitHub; setting `poll_interval` trades that for not needing an inbound route (ADR 0023).
