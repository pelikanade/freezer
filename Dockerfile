# Freezer: webhook-driven Nix builds published to an S3-compatible cache (Cloudflare R2).
#
#   podman build -t freezer:0.1.0 .
#
# Stage 1 compiles the Go package with the official Go toolchain; stage 2 is the
# runtime, on the official Nix image, which ships Nix 2.34.8, git, tar and curl
# on PATH — everything freezer shells out to. Building the image runs no Nix, so
# no sandbox, user-namespace mapping or store is involved in making it.

FROM docker.io/library/golang:1.26.7-bookworm AS build

WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY web ./web
COPY config.go loop.go main.go service.go state.go web.go ./
# The flags follow nix/package.nix (CGO_ENABLED=0, stripped), so this binary and
# the one `nix build .#default` produces are the same program.
RUN CGO_ENABLED=0 go build -trimpath -ldflags '-s -w' -o /out/freezer .

FROM docker.io/nixos/nix:2.34.8

COPY --from=build /out/freezer /usr/local/bin/freezer

# In the base image /etc/nix/nix.conf is a symlink into the read-only store;
# replace the symlink so the container's settings do not edit store contents.
# build-users-group is empty: this is one single-user store inside one
# container, so builds run as the container's user inside the sandbox instead
# of as the image's nixbld users — which would have to be mapped through the
# deploying host's subuid range. sandbox = true with sandbox-fallback = false
# means builds are sandboxed, and a runtime that cannot give the container root
# the namespaces for it fails loudly rather than building without a sandbox.
RUN rm /etc/nix/nix.conf \
 && printf '%s\n' \
      'build-users-group =' \
      'sandbox = true' \
      'sandbox-fallback = false' \
      'experimental-features = nix-command flakes' \
      'trusted-public-keys = cache.nixos.org-1:6NCHdD59X431o0gWypbMrAURkbJ16ZPMQFGspcDShjY=' \
      > /etc/nix/nix.conf \
 && mkdir -p /etc/freezer /var/lib/freezer/cache

ENV PATH=/usr/local/bin:/root/.nix-profile/bin:/nix/var/nix/profiles/default/bin:/nix/var/nix/profiles/default/sbin \
    HOME=/var/lib/freezer \
    XDG_CACHE_HOME=/var/lib/freezer/cache \
    GIT_TERMINAL_PROMPT=0 \
    AWS_EC2_METADATA_DISABLED=true

# The container contract: the mounted config listens on 0.0.0.0:8080.
# Podman builds OCI-format images, which drop HEALTHCHECK; deploy/freezer.container
# carries the same check for that deployment.
EXPOSE 8080
HEALTHCHECK --interval=30s --timeout=5s --start-period=15s \
  CMD curl -fsS http://127.0.0.1:8080/ > /dev/null || exit 1

ENTRYPOINT ["/usr/local/bin/freezer"]
CMD ["/etc/freezer/freezer.yaml"]
