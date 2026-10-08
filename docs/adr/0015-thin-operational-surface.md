# The operational surface is deliberately thin

Several things a build service normally has are deliberately absent. Each was considered and declined for the same reason: a single maintainer would rather have a smaller program than a more self-managing one.

- **No automatic retries.** A failed job is failed; recovery is the "build now" action. Classifying nix's unstructured errors into retryable and non-retryable buckets is guesswork, and retrying hides flakiness instead of exposing it.
- **No metrics endpoint.** The UI, the logs, and GitHub commit statuses are the observability. No second surface to secure, staff, or keep honest.
- **One retention setting.** "Keep the last N jobs per repository", not a policy with day- and count-based knobs. Old rows and their log blobs are deleted together.
- **No store garbage collection.** Freezer never prunes the local nix store; that is the operator's business, and a build service deleting store paths on a machine it shares with other things is a bad default.
- **No preflight validation of the host's nix configuration** (see ADR 0008): nix's own errors, surfaced per job, are the report.

## Consequences

- Disk growth — both the SQLite file and the nix store — is the operator's responsibility, with no warnings from the service itself.
- Flaky failures (a transient network fetch during a build) become manual re-runs.
- Anything that goes wrong for long enough is noticed by looking at the UI or at commits, not by an alert.
