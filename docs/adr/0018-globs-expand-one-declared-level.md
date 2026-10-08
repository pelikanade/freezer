# Globs expand one declared level, and nothing beyond it

An entry whose final component is a glob (`packages.x86_64-linux.*`) names a pattern rather than a job. Expansion is one `nix eval --json "<flake>#<prefix>" --apply builtins.attrNames` call per pattern, and the rules that keep it from turning into open-ended evaluation are the whole of this decision:

- **Only the final component may be a glob.** `packages.x86_64-linux.*` is legal; `packages.*.*` and `*.<system>.foo` are not. One `attrNames` call per pattern, no recursion, no crawling.
- **Only declared patterns are evaluated.** The flake's output tree is never enumerated as a whole, so an unmentioned `legacyPackages` costs nothing — the useful half of garnix's behaviour, without its hardcoded parent list.
- **Import-from-derivation stays off** during expansion, via `--option allow-import-from-derivation false` — accepted per invocation, and it fails loudly instead of building (verified). Expansion may evaluate repository code; it may not build it.
- **A cap on jobs per event** (config, default in the low hundreds). Exceeding it fails that pattern's entries with a clear message rather than building the first N — a silently truncating cap would make the build list a lie.
- **A failing expansion fails every entry under that pattern**, and only those. A pattern that matches nothing is not an error; a prefix that does not exist is.

## Consequences

- Repository code runs before the job list exists. This is the one exception to ADR 0001's invariant, and it is confined to enumerating attribute names inside a prefix the repository itself declared — which is why ADR 0001 states that invariant in terms of the pattern space rather than the resolved list.
- Expansion is per pattern, so two overlapping patterns enumerate a shared prefix twice. Accepted: the call is cheap, and an expansion cache brings invalidation rules that cost more than they save.
- The cap is the mechanism that stops a glob from turning one push into an unbounded amount of work; it is therefore the first thing to raise when a legitimate repository hits it, and the fact that it must be raised manually is deliberate.
