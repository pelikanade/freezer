# Planned jobs are recorded when the event starts

The UI spec forbids inventing a persisted status without a decision, so this is one: when an event starts, every entry in its plan gets a job record immediately, in the order the build list declares, with the status `planned`. A job flips to `running` when it takes a global build slot; if its event ends before that, it becomes `stopped`, and if the server restarts first it becomes `interrupted`.

The reason is the same one that shapes the rest of the control surface: the UI is the only surface, and the plan used to live in memory only. With `global_build_cap` at 2 and a build list of sixteen entries, fourteen of them were invisible until their turn came — which, from the operator's side, is indistinguishable from entries that had been dropped. Recording them up front makes the whole event legible while it runs, gives every entry a page of its own, and keeps one source of truth: the SQLite file (ADR 0014), not a second rendering path over in-memory state.

## Consequences

- An event's history therefore contains rows for work it never started, and they say so: `stopped` with "The event ended before this job started.", or `interrupted` when a restart got there first. Neither counts as a failure, and neither records a commit.
- `started_at` is zero until a job starts, and the index and job pages show a dash instead of a time — a planned job has a place in the order, not a start.
- Retention counts planned rows like any other, so a stopped event's unstarted work ages out with the rest of it.
- "Queued" and "planned" are different things and the UI says both: the queued row is the one *waiting event* for a repository, which has no jobs at all until it starts; planned rows are the *running* event's jobs that have not taken a slot.
- A planned row is not protected from retention the way a running one is (ADR 0019), and it does not need to be: pruning only happens between events, when nothing is planned.

## Considered Options

- **Rendering the in-memory plan without rows.** Rejected: no job ids, no pages, no transcripts, and a second rendering path beside the database that ADR 0014 deliberately made the only state.
- **A separate table for planned work.** Rejected: the same data with a second lifecycle, joined back into every page that lists jobs.
- **Showing a count only** ("14 waiting"). Rejected: the operator still cannot see *which* entries are waiting or open one, which is the whole complaint.
