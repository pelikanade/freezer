# The repository owns the build list; the server owns everything else

A repository declares what may be built from it (`.freezer.yaml`, containing only build list entries — attribute paths, optionally globbed) and nothing else. Registry membership, ref policy, credentials, cache target, concurrency, and the signing key are all server-side.

The rule this encodes: in-repo configuration may *narrow* what the server does, never *grant*. A hostile or broken repository config cannot exfiltrate credentials or redirect uploads, because it never had access to them.

A second, less obvious consequence: the build list is a file, parsed without evaluating any nix, so the *pattern space* — which attribute sets may even be looked inside — is fixed before any repository code runs. The single exception is glob expansion, which must enumerate a prefix the file itself declared; ADR 0018 bounds it. The invariant is therefore about the patterns, not about the resolved list.

## Consequences

- A missing or unparseable `.freezer.yaml` produces no entries at all: the event fails with a red commit status and nothing is built. Reading configuration is not a build, so it must never be able to build anything by accident.
- The only repository code that runs before the job list exists is attribute-name enumeration of a declared prefix (ADR 0018). Everything else the repository contains is reachable only through an entry that names it.

## Considered Options

- **Build list expressed as a flake output** (`nix eval .#freezer.config`): rejected, because computing the list requires evaluating repository code, which inverts the trust relationship — the list can no longer bound what is evaluated.
- **Server-side build lists** (edited in the admin surface): rejected, because it duplicates what git already versions, and drift between the repo and the server becomes invisible.
