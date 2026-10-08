# Dependencies: a pure-Go SQLite driver and yaml.v3, and nothing else

The standard library covers almost everything: `net/http` and `html/template` for the UI, `encoding/json` for webhook payloads and API responses, `crypto/hmac` for webhook verification, `os/exec` for git and nix, `bufio` and `os` for everything else. Exactly two non-standard dependencies are accepted:

- **GORM** (`gorm.io/gorm`) with a pure-Go SQLite dialect (`github.com/glebarez/sqlite`) — no CGO, and its `AutoMigrate` replaces hand-written schema management;
- `gopkg.in/yaml.v3`, for the server config and the repository build lists.

Deliberately absent, each for a specific reason:

- **No AWS SDK.** All bucket access is nix's (ADR 0010), so there is no signing, no multipart, and no retry policy for us to own.
- **No web framework or router.** `net/http`'s mux is enough for a handful of GET routes plus one webhook POST.
- **No git library.** Shelling out to `git fetch` is smaller than a Git implementation and matches how the server already talks to nix.

## Consequences

- Reaching for a dependency requires beating "write it with the standard library" by a wide margin, and the exemption gets recorded here. GORM is the largest thing in `go.sum`, accepted because it removes hand-written schema and query code rather than adding a layer over it.
- CGO stays off, so the binary builds statically and the nix package needs no C toolchain.
- Code layout follows from the same instinct: one Go package, a handful of files, no `internal/` tree to navigate.
