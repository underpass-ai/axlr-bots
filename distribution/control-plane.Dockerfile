# syntax=docker/dockerfile:1
# The control plane image: axlr-bots (serve) and AXLR's axlr-tui, for the
# runs the control plane makes in process when no node can take them.
# Both stages that do work run on the build platform; the target stage
# only copies, so linux/amd64 and linux/arm64 build without emulation.
#
#   docker buildx build -f distribution/control-plane.Dockerfile --platform linux/arm64 \
#     --build-arg VERSION=0.4.0 --build-arg REVISION=$(git rev-parse HEAD) \
#     -t axlr-bots:dev --load .
#
# VERSION is the release without a leading v (the image tag); REVISION
# and BUILD_TIME name the commit. The context has no .git, so these are
# what GET /v1/build and `axlr-bots version` report (internal/buildinfo).

ARG GO_IMAGE=golang:1.26.10-trixie@sha256:a050dcceee6eb9bf110151662dfac7662f3bec40927c7dee48731e3d92083d42
ARG BASE_IMAGE=gcr.io/distroless/static-debian13:nonroot@sha256:18f4cc26c9ffa7f20a359298be7deadac5c607c8e03a02b8119f1a5fc2f82c99

FROM --platform=$BUILDPLATFORM ${GO_IMAGE} AS build
ARG TARGETARCH
ARG BUILDARCH
# The image keeps transcripts in SQLite: the sqlite build tag, through
# cgo (adapters/sqlitestore, docs/architecture/transcripts.md); this stage
# runs on the build platform, so for another target it installs that
# target's C cross compiler. The binary is linked statically, so the
# distroless static base still runs it; netgo and osusergo keep glibc's
# resolver and user lookups out, and sqlite_omit_load_extension its
# dlopen.
RUN set -eu; \
    if [ "$TARGETARCH" != "$BUILDARCH" ]; then \
      case "$TARGETARCH" in \
        arm64) pkgs="gcc-aarch64-linux-gnu libc6-dev-arm64-cross" ;; \
        amd64) pkgs="gcc-x86-64-linux-gnu libc6-dev-amd64-cross" ;; \
        *) echo "no C cross compiler for $TARGETARCH" >&2; exit 1 ;; \
      esac; \
      apt-get update; \
      apt-get install -y --no-install-recommends $pkgs; \
      rm -rf /var/lib/apt/lists/*; \
    fi
WORKDIR /src
COPY go.mod go.sum ./
COPY adapters ./adapters
COPY application ./application
COPY cmd ./cmd
COPY compose ./compose
COPY domain ./domain
COPY internal ./internal
COPY nodeproto ./nodeproto
ARG TARGETOS=linux
# GOFLAGS=-p=2 on a small builder.
ARG GOFLAGS=""
ARG VERSION=""
ARG REVISION=""
ARG BUILD_TIME=""
# No -trimpath: with it Go records no -ldflags, and the daemon could not
# read the stamp back from the binary on disk (internal/buildinfo).
RUN --mount=type=cache,target=/root/.cache/go-build \
    set -eu; \
    cc=gcc; \
    if [ "$TARGETARCH" != "$BUILDARCH" ]; then \
      case "$TARGETARCH" in arm64) cc=aarch64-linux-gnu-gcc ;; amd64) cc=x86_64-linux-gnu-gcc ;; esac; \
    fi; \
    pkg=github.com/underpass-ai/axlr-bots/internal/buildinfo; \
    CGO_ENABLED=1 CC=$cc GOOS=$TARGETOS GOARCH=$TARGETARCH GOFLAGS=$GOFLAGS \
    go build -tags "sqlite sqlite_omit_load_extension netgo osusergo" \
      -ldflags="-s -w -linkmode external -extldflags -static -X $pkg.Version=$VERSION -X $pkg.Revision=$REVISION -X $pkg.Time=$BUILD_TIME" \
      -o /out/axlr-bots ./cmd/axlr-bots

# AXLR's release archive for the target, checked against the sha256 its
# release publishes beside it (axlr-v<version>-linux-<arch>.tar.gz.sha256).
# The same pin as node.Dockerfile: CI refuses the two drifting apart.
FROM --platform=$BUILDPLATFORM ${GO_IMAGE} AS axlr
ARG TARGETARCH
ARG AXLR_VERSION=0.16.0
ARG AXLR_SHA256_AMD64=7463afa066e87bd7f2629c748284a783b79100946cfa5d8f864830cee487ec46
ARG AXLR_SHA256_ARM64=a5445c7d698922ac978b58b37afacf5397d89b258ebd29409b942db5a465a1c4
ADD https://github.com/underpass-ai/AXLR/releases/download/v${AXLR_VERSION}/axlr-v${AXLR_VERSION}-linux-${TARGETARCH}.tar.gz /tmp/axlr.tar.gz
RUN set -eu; \
    case "$TARGETARCH" in \
      amd64) sum="$AXLR_SHA256_AMD64" ;; \
      arm64) sum="$AXLR_SHA256_ARM64" ;; \
      *) echo "AXLR publishes no linux archive for $TARGETARCH" >&2; exit 1 ;; \
    esac; \
    echo "$sum  /tmp/axlr.tar.gz" | sha256sum -c -; \
    mkdir -p /out/licenses; \
    tar -xzf /tmp/axlr.tar.gz -C /out axlr-tui VERSION; \
    tar -xzf /tmp/axlr.tar.gz -C /out/licenses LICENSE NOTICE THIRD_PARTY_LICENSES; \
    test "$(cat /out/VERSION)" = "$AXLR_VERSION"

FROM ${BASE_IMAGE}
ARG VERSION=""
ARG REVISION=""
LABEL org.opencontainers.image.title="axlr-bots" \
      org.opencontainers.image.description="AXLR Bots control plane (axlr-bots serve)" \
      org.opencontainers.image.source="https://github.com/underpass-ai/axlr-bots" \
      org.opencontainers.image.licenses="Apache-2.0" \
      org.opencontainers.image.version="${VERSION}" \
      org.opencontainers.image.revision="${REVISION}"
COPY --from=build /out/axlr-bots /usr/local/bin/axlr-bots
COPY --from=axlr /out/axlr-tui /usr/local/bin/axlr-tui
COPY LICENSE NOTICE /licenses/axlr-bots/
COPY --from=axlr /out/licenses/ /licenses/axlr/
# The state is a volume: bots, tasks, the fleet CA, the operator token,
# the socket. AXLR_BOTS_STATE names it, so `axlr-bots health` (the
# healthcheck) finds the socket at <state>/axlr-bots.sock with no flag;
# a deployment that mounts the state elsewhere sets AXLR_BOTS_STATE to
# that path. An in-process run keeps AXLR's own state under /tmp (a
# tmpfs), as the node image does: AXLR refuses a state directory with an
# ancestor other accounts may write.
ENV AXLR_TUI=/usr/local/bin/axlr-tui \
    AXLR_BOTS_STATE=/var/lib/axlr-bots \
    HOME=/var/lib/axlr-bots/home \
    XDG_STATE_HOME=/tmp/axlr-state
USER 65532:65532
WORKDIR /var/lib/axlr-bots
# The node protocol (mutual TLS) and the operator API (bearer token).
EXPOSE 7878 7879
HEALTHCHECK --interval=30s --timeout=5s --start-period=20s --retries=3 \
    CMD ["/usr/local/bin/axlr-bots", "health"]
ENTRYPOINT ["/usr/local/bin/axlr-bots"]
CMD ["--actor", "control-plane", "serve"]
