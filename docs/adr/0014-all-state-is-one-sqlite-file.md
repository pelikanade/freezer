# All state is one SQLite file

Job records, job logs, and per-repository build state (last built commit, last result, timestamps) live in a single SQLite database in the state directory, reached through GORM with a pure-Go dialect (no CGO); the schema is created and evolved by `AutoMigrate` on startup. There is no separate state file format and no log directory. No key material is in the database — the signing key is a file (ADR 0012).

For a single maintainer this is the whole story: one artifact to copy, back up, and open with `sqlite3`; no directory-layout conventions to invent; deleting old data is one statement driven by a single retention number.

## Consequences

- The database grows with build output. Job logs are stored verbatim with **no per-job size cap** — a cap was considered and declined — so retention (keep the last N jobs per repository) is the only bound, and one pathological build can inflate the file until it ages out.
- Reclaiming space means vacuuming a live database, which is an operational action rather than a background behaviour.
- Concurrent jobs write job records and log lines into one file, and SQLite permits a single writer at a time. Those writes are serialized through one path rather than retried: contention is a property of the storage choice, not something to paper over with retry loops.
- Repository definitions are deliberately *not* here: they live in the config file (ADR 0011). One source of truth for what is built, one for what happened, and no reconciling between them.

## Considered Options

- Log files on disk with metadata in the database: rejected in favour of the maintainer having one thing to look at.
- A database per repository: rejected as N artifacts to back up for no gain at this scale.
- No history at all: rejected, since the UI's job list is the point of the UI.
