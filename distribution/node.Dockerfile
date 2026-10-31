# syntax=docker/dockerfile:1
# The node image: axlr-node and AXLR's axlr-tui, what a sidecar runs.
# Both stages that do work run on the build platform; the target stage
# only copies, so linux/amd64 and linux/arm64 build without emulation.
#
#   docker buildx build -f distribution/node.Dockerfile --platform linux/arm64 -t axlr-bots-node:dev --load .

ARG GO_IMAGE=golang:1.26.5-trixie@sha256:f1a132429b98724a904e9b3bdbaed399d8f923203c3e5170e6def66d0a7cc04c
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
RUN --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH GOFLAGS=$GOFLAGS \
    go build -trimpath -ldflags="-s -w" -o /out/axlr-node ./cmd/axlr-node

# AXLR's release archive for the target, checked against the sha256 its
# release publishes beside it (axlr-v<version>-linux-<arch>.tar.gz.sha256).
FROM --platform=$BUILDPLATFORM ${GO_IMAGE} AS axlr
ARG TARGETARCH
ARG AXLR_VERSION=0.15.0
ARG AXLR_SHA256_AMD64=7c91b412a08c38d7e7937f444488adc890099ddd59ade3a5dd80f3bae252f1d3
ARG AXLR_SHA256_ARM64=f9dfb7df38ebcf5f965bdceef6835a145a06f22b82a144e5c255c2d43cd94fe4
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
