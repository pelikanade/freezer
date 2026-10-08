# Only the default branch is built; fork PRs never are

A job runs code on the host alongside the nix daemon, and anything it produces is published *signed under the server's own key* — which makes the server's published paths trusted by every consumer. A pull request from an untrusted author therefore does not merely consume build resources; it can inject attacker-chosen store paths into a trusted cache.

So the ref policy is the default branch, and nothing else. Not fork PRs, not same-repo branches, and not tags unless the policy is deliberately widened later.

## Consequences

- No pre-merge verification, which is the main reason a self-hosted builder is attractive to teams in the first place. Widening the policy later requires real isolation (no secrets in the builder environment, ephemeral signing, container or VM boundaries) before it is defensible — it is not a config flip.
- Webhook handling reduces to: verify HMAC, detect pushes to the default branch (including force-pushes), and re-resolve the ref server-side rather than trusting the payload's commit.
- A repository enters the registry only when the operator adds it to the server config and restarts (ADR 0011); the webhook is checked against that list, so an unregistered repository can never trigger a build.
- The branch built is the repository's default branch, read from git rather than from GitHub: `git ls-remote --symref <url> HEAD` reports the ref that `HEAD` points at (verified against a repository whose default branch is `trunk`, with three branches present, so a lucky guess cannot pass). The push payload's `ref` is used only to decide whether the push touched that branch — no API call, no reliance on an undocumented payload field. It is still not a per-repository config field: a configurable branch would make this policy a config flip, which is exactly what this decision says it is not.
