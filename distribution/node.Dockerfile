# syntax=docker/dockerfile:1
# The node image: axlr-node and AXLR's axlr-tui, what a sidecar runs.
# Both stages that do work run on the build platform; the target stage
# only copies, so linux/amd64 and linux/arm64 build without emulation.
#
#   docker buildx build -f distribution/node.Dockerfile --platform linux/arm64 -t axlr-bots-node:dev --load .

ARG GO_IMAGE=golang:1.26.10-trixie@sha256:a050dcceee6eb9bf110151662dfac7662f3bec40927c7dee48731e3d92083d42
ARG BASE_IMAGE=gcr.io/distroless/static-debian13:nonroot@sha256:18f4cc26c9ffa7f20a359298be7deadac5c607c8e03a02b8119f1a5fc2f82c99

FROM --platform=$BUILDPLATFORM ${GO_IMAGE} AS build
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
ARG TARGETARCH
# GOFLAGS=-p=2 on a small builder.
ARG GOFLAGS=""
# The release, without a leading v, and its commit (internal/buildinfo);
# the context has no .git to read them from.
ARG VERSION=""
ARG REVISION=""
ARG BUILD_TIME=""
# No -trimpath: with it Go records no -ldflags, and the daemon could not
# read the stamp back from the binary on disk (internal/buildinfo).
RUN --mount=type=cache,target=/root/.cache/go-build \
    set -eu; \
    pkg=github.com/underpass-ai/axlr-bots/internal/buildinfo; \
    CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH GOFLAGS=$GOFLAGS \
    go build \
      -ldflags="-s -w -X $pkg.Version=$VERSION -X $pkg.Revision=$REVISION -X $pkg.Time=$BUILD_TIME" \
      -o /out/axlr-node ./cmd/axlr-node

# AXLR's release archive for the target, checked against the sha256 its
# release publishes beside it (axlr-v<version>-linux-<arch>.tar.gz.sha256).
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
LABEL org.opencontainers.image.title="axlr-bots-node" \
      org.opencontainers.image.description="AXLR Bots node agent (axlr-node) with axlr-tui" \
      org.opencontainers.image.source="https://github.com/underpass-ai/axlr-bots" \
      org.opencontainers.image.licenses="Apache-2.0" \
      org.opencontainers.image.version="${VERSION}" \
      org.opencontainers.image.revision="${REVISION}"
COPY --from=build /out/axlr-node /usr/local/bin/axlr-node
COPY --from=axlr /out/axlr-tui /usr/local/bin/axlr-tui
COPY LICENSE NOTICE /licenses/axlr-bots/
COPY --from=axlr /out/licenses/ /licenses/axlr/
# The state is a volume: the credentials, the traces, the policies and
# the workspaces survive a restart. AXLR refuses a state directory (its
# diagnostics and sessions) with an ancestor other accounts may write
# unless it is sticky, and a volume's root is 0777 or group-writable
# under fsGroup; /tmp is a tmpfs (1777), in Kubernetes an emptyDir with
# medium Memory.
ENV AXLR_TUI=/usr/local/bin/axlr-tui \
    AXLR_NODE_STATE=/var/lib/axlr-node \
    HOME=/var/lib/axlr-node/home \
    XDG_STATE_HOME=/tmp/axlr-state
USER 65532:65532
WORKDIR /var/lib/axlr-node
ENTRYPOINT ["/usr/local/bin/axlr-node"]
