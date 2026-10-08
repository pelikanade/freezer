# The GitHub API base is configuration

The service takes a GitHub API base URL, defaulting to `https://api.github.com`, which is where commit statuses are posted. Its purpose is that the service tests can point status posting at a local HTTP server and assert the requests that would have gone to GitHub.

The alternative was a Go interface with a fake implementation. That was rejected for the same reason the S3 side needs no interface: an in-process fake cannot fail on the things that actually break here — the request URL, the authentication header, the payload shape — while a real HTTP request to a local server exercises all of them. It also keeps the design's rule about substitutes honest: substitutes are network stand-ins for real services (ADR 0020), never code-level doubles.

## Consequences

- One more configuration field, which must be defaulted and validated as a URL. It is documented as a seam that exists for testing, not as a user-facing knob.
- It does **not** reopen the "GitHub only" decision: having a base URL configured is not the same as supporting another forge, and no other forge's API is implemented or tested.
- The same seam is what would make GitHub Enterprise possible later, which is exactly why it is worth a sentence rather than being smuggled in as a test hook.
