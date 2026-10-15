# No authentication of our own: the admin UI rides on the proxy

The server has no login, no sessions, no OAuth, and no authorization list. The admin UI is served over plain HTTP with access control delegated entirely to whatever fronts it: the reverse proxy that already terminates TLS, or a localhost bind reached over SSH.

The repository registry is not an API: repositories are declared in the server's config file, so adding one is a config edit by the person who runs the server rather than a privileged HTTP call.

The UI exposes write actions — build now, stop a build — unauthenticated, protected only by that front. Accepted deliberately: the alternative was an OAuth app registration, a session store, an allowlist, and CSRF handling, in a service whose operator has shell access to the machine anyway. OAuth was in fact the earlier decision, and it was the first one dropped when "simple" was tightened to mean less code rather than fewer features.

## Consequences

- Anything that can reach the UI can trigger or stop a build. If the front is misconfigured, that is the entire exposure; there is no second layer.
- There is no per-user audit trail, and the GitHub token used to post commit statuses is a single shared service credential.
- Because there are no cookies, there is no CSRF story to get wrong — but also no session to revoke.
- The binary has no subcommands and a single mode: it serves. Its only argument is the path of the config file (a mount in the container deployment supplies it), and the UI is the entire control surface — build now, stop, transcripts, job history. Anything the UI cannot do is a config edit and a restart.
- With no CLI, an unreachable UI means the only manual trigger left is a push to the branch, which the next poll turns into a build. That is a small loss: shell access to the box no longer helps you start a build.
