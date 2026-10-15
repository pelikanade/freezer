# Triggering can also poll the branch head

ADR 0013 decided triggering was webhook-only and rejected a periodic sweep. That stands for a deployment GitHub can reach. It does not for one GitHub cannot reach: the webhook route is the *only* thing in this service that has to be publicly reachable, and keeping a whole host off the public internet is worth more to a single maintainer than the polling traffic a sweep costs.

So: `poll_interval`, off when absent or zero. With it set, the server resolves every registered repository's default-branch head on that interval — at once when it starts, then every interval — and starts the same build event a delivered push would have started. The webhook route stays: it is not removed for a deployment that has both, and the two paths need no coordination, because the event path is idempotent — the last-built check ends any event whose head is already built.

Nothing downstream of the event changes: the same resolution when the event starts (ADR 0004), the same last-built check (ADR 0019), the same commit statuses out (ADR 0013). A poll costs one `git ls-remote` against the repository per interval, and events cost what they would have cost anyway.

## Consequences

- A push is found at the next tick rather than immediately, so a build starts later by up to one interval. That is the trade for needing no inbound route at all.
- This *is* the reconcile sweep ADR 0013 declined to build, and it closes the gap ADR 0013 accepted: a push that arrives while the server is down is found by the next poll instead of waiting for Build now. A skipped push is no longer silently lost.
- A poll that cannot reach the remote records the failure in the repository's state — the UI shows it — and posts no commit status, because no commit was resolved. A GitHub or network outage therefore looks like a failing repository, which it is: the service could not do its job.
- Polled events are ordinary events, so the per-repository queue governs them: while a build runs, later polls coalesce into the one waiting event and nothing piles up.
- A waiting Build now is never displaced by a non-forced event (ADR 0019). Polling forced that rule to be written down: without it, a tick could replace the repair path with an ordinary event, and the repair would quietly degrade into a probe.
- A repository's `webhook_secret` is now optional, and an empty one means "no webhook route": every delivery to it is refused with 403. Without that rule a polling-only deployment would have to invent a secret it never uses, and an empty key would make every signature forgeable. Omitting the field is therefore the honest way to turn the route off (ADR 0013's per-repository secret is unchanged for repositories that do take deliveries).
- With `poll_interval` set and no webhook registered, the service needs no public route at all. The UI stays behind the private network or an SSH tunnel, which is what ADR 0011 already assumes its proxy provides.

## Considered Options

- **Keeping webhook-only and exposing the route.** Rejected for this deployment: it makes the service reachable from the internet for the sake of the trigger, which is the single largest exposure here and the only part a private host cannot keep private.
- **The GitHub API with a `since` query instead of `ls-remote`.** Rejected: it would add a second remote protocol (the API) to a service whose remote reads are git, and the last-built check already makes a plain resolution cheap and idempotent.
- **Making polling the default.** Rejected: a webhook is lower latency and costs only GitHub's side, so an existing deployment should not silently start polling. It stays off until configured.
- **A durable event queue instead of polling.** Rejected as before (ADR 0013): a queue cannot find a push whose webhook was never sent, which is exactly the problem here.
