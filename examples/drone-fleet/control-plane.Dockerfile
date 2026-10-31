# The AXLR Bots control plane (axlr-bots) in a container, for the port
# fleet's own control plane in its namespace. Built from this repository's
# root: docker build -f examples/drone-fleet/control-plane.Dockerfile .
# The base is distroless's debug variant only for its busybox shell, which
# the pod's first start uses to run `ca init` once.
ARG GO_IMAGE=golang:1.26.5-trixie@sha256:f1a132429b98724a904e9b3bdbaed399d8f923203c3e5170e6def66d0a7cc04c
ARG BASE_IMAGE=gcr.io/distroless/static-debian13:debug-nonroot@sha256:c7b8faacc4439b8f5c41ca7ce4a3f8ff3e8520cb0c3444fae7292f687b00d0dd

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
ARG GOFLAGS=""
RUN --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH GOFLAGS=$GOFLAGS \
    go build -trimpath -ldflags="-s -w" -o /out/axlr-bots ./cmd/axlr-bots

FROM ${BASE_IMAGE}
COPY --from=build /out/axlr-bots /usr/local/bin/axlr-bots
COPY LICENSE NOTICE /licenses/axlr-bots/
ENV HOME=/var/lib/axlr-bots
USER 65532:65532
WORKDIR /var/lib/axlr-bots
ENTRYPOINT ["/usr/local/bin/axlr-bots", "--state", "/var/lib/axlr-bots"]
