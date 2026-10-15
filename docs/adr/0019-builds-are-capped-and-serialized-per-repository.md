# Builds are capped globally, serialized per repository, and superseded events are dropped

Scheduling, which no earlier document decided:

- **One event per repository runs at a time.** Jobs inside an event run concurrently, bounded by a global cap (config); different repositories may run simultaneously.
- **A newer event for a repository supersedes queued, not yet started events for that repository.** Rapid pushes cost one build of the newest commit, not one per push. A running event is not cancelled by a newer one: it finishes, and the newer commit is built afterwards — the reconcile-free equivalent of keeping up with the branch head. The one exception is a waiting Build now, which no non-forced event displaces: the repair path must not be degraded into a probe by a poll tick (ADR 0023).
- **The recorded last-built commit de-duplicates polls of an unchanged head.** An event whose commit equals the recorded one, and whose previous run succeeded, is dropped without touching the commit status.
- **Events are an in-memory list, not a durable queue.** After a restart nothing is pending (ADR 0013).

## Consequences

- One repository cannot occupy the whole machine, and one repository's backlog cannot block another's.
- The per-repository serialization is what makes a hung build expensive: it blocks that repository's later commits until the job timeout or a manual stop fires, which is why both exist.
- A superseded event leaves no trace — no commit status is posted for a commit that was coalesced away. The status on the newest commit is the report for the repository, and ADR 0013 already records that silence is therefore ambiguous.
- Jobs inside one event write records and transcript lines into a single SQLite file, which permits one writer at a time; those writes are serialized through one path rather than retried (ADR 0014).
