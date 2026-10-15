# Triggering is polling-only; there is no webhook route

ADR 0023 added polling *beside* the webhook. This removes the webhook: there is no inbound route, no per-repository secret, no signature check, and no push payload to parse. A build event comes from a poll of the default-branch head, or from the operator pressing Build now.

The webhook's whole job was to tell the service that something was pushed. Polling already answers that question on the interval, and only the webhook needed the service to be reachable from the internet — which is the one thing a single maintainer's host should not have to be. Keeping the route "just in case" would have meant keeping an inbound write surface, a per-repository HMAC key and a signature path that nothing exercised, and whose only users were tests.

## Consequences

- **`poll_interval` is required and positive.** Polling is the only automatic trigger, so a zero interval would mean a service that never builds on its own; the config load refuses it rather than starting something inert.
- **The service needs no inbound route at all.** ADR 0011's exception — that the signed webhook route had to be publicly reachable — is gone. The UI can sit on a private network or behind an SSH tunnel, and nothing exposes a build trigger to the internet. What left the host is unchanged: `git ls-remote`, the build's own fetches, and commit statuses.
- **A push is found within one interval, not immediately.** ADR 0023 accepted that trade; it is now the only latency the trigger has.
- **A repository's configuration is its URL and slug.** The webhook secret, the signature comparison, the `X-GitHub-Event` filter, the payload parsing and the non-default-branch drop are all deleted, along with the branch check that read `HEAD` on every delivery.
- **Deletion and non-default-branch pushes are invisible by construction.** There is no payload to inspect: the poll reads the default branch's head, and a repository whose default branch was deleted fails head resolution, which shows as a failed repository state with no commit status.
- **A push that lands while the service is down is not lost.** The next poll finds it. This is the gap ADR 0013 accepted and ADR 0023 closed; removing the webhook leaves no path that can lose one.
- **The queue's one waiting event now only ever holds a poll or a Build now**, which is why `enqueue` had to keep Build now from being displaced (ADR 0019): a tick would otherwise silently turn a repair into a probe.
- **Tests trigger by starting the service.** The poll at startup is the first event, `1h` intervals keep the rest of a test quiet, and the tests that need a second automatic event use a one-second interval and a new commit; there is no test seam and no HTTP route to fake.
- **What the queued row shows changed.** The index still shows a waiting event's age, but no pushed sha: an event no longer carries one, and the head is resolved when it runs (the UI spec's "if the event carries one" is now never true).

## Considered Options

- **Keeping the route but never registering it.** Rejected: an unused inbound surface with a signature check and a per-repository key is exactly the code that rots, and its tests would have to be kept alive for a path no deployment uses.
- **A local trigger instead** (a file drop, a CLI subcommand, a loopback-only endpoint). Rejected: the poll already covers "something changed", and a second trigger path is a second thing to document, secure and test — the reason this repository has no CLI at all.
- **A manual ordinary "check for changes now" action in the UI.** Not built: Build now already gives an operator who cannot wait an immediate result, at the cost of skipping the probe, and the next tick is at most one interval away. Adding it would put two manual actions with different semantics side by side for one impatient case.
- **Requiring no secret and rejecting deliveries, as ADR 0023's last revision did.** Rejected as half-measures: the route, the HMAC code and the secret field all remain, kept alive by a rule whose only purpose was to make them harmless.
