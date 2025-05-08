ARG DOCKER_REGISTRY=ghcr.io
ARG DOCKER_REGISTRY_GOOGLE=gcr.io
ARG TOOLS_UV_VERSION=latest
ARG PROXY
ARG NOPROXY

### STAGE 1: base image
FROM --platform=$BUILDPLATFORM $DOCKER_REGISTRY/astral-sh/uv:${TOOLS_UV_VERSION}-bookworm-slim AS base-os
ARG TOOLS_NODE_VERSION

#ENVIRONMENT VARIABLES
ENV DEBIAN_FRONTEND=noninteractive

RUN mkdir -p /etc/apt/keyrings \
    && apt-get update && apt-get install -y --no-install-recommends \
    git                       \
    ca-certificates           \
    curl                      \
    gnupg                     \
    wget                      \
    && update-ca-certificates \
    && rm -rf /var/lib/apt/lists/* \
    && ln -sf /usr/bin/python3 /usr/bin/python

FROM --platform=$BUILDPLATFORM base-os AS go-base
ARG TOOLS_GO_VERSION
ARG ARCH=${TARGETARCH:-amd64}
ARG GOPROXY="http://proxy.golang.org"

ENV GO111MODULE=on
ENV GOTOOLCHAIN=local
ENV PATH=$PATH:/usr/local/go/bin

RUN --mount=type=cache,target=/cache,rw \
    wget -q --show-progress -P /cache https://golang.org/dl/go${TOOLS_GO_VERSION}.linux-${ARCH}.tar.gz && \
    tar     -C /usr/local -xzf /cache/go${TOOLS_GO_VERSION}.linux-${ARCH}.tar.gz

FROM --platform=$BUILDPLATFORM go-base AS builder

ARG TARGETPLATFORM
ARG BUILDPLATFORM
ARG TARGETOS
ARG TARGETARCH

WORKDIR /workspace
# Copy the Go Modules manifests
COPY go.mod go.mod
COPY go.sum go.sum
# cache deps before building and copying source so that we don't need to re-download as much
# and so that source changes don't invalidate our downloaded layer
RUN --mount=type=cache,target=/root/go/pkg/mod,rw      \
    --mount=type=cache,target=/root/.cache/go-build,rw \
    CGO_ENABLED=0 GOOS=${TARGETOS:-linux} GOARCH=${TARGETARCH} go mod download

# Copy the go source
COPY autogen autogen
COPY controller controller

# Build
# the GOARCH has not a default value to allow the binary be built according to the host where the command
# was called. For example, if we call make docker-build in a local env which has the Apple Silicon M1 SO
# the docker BUILDPLATFORM arg will be linux/arm64 when for Apple x86 it will be linux/amd64. Therefore,
# by leaving it empty we can ensure that the container and binary shipped on it will have the same platform.
RUN --mount=type=cache,target=/root/go/pkg/mod,rw      \
    --mount=type=cache,target=/root/.cache/go-build,rw \
    CGO_ENABLED=0 GOOS=${TARGETOS:-linux} GOARCH=${TARGETARCH} go build -a -o manager controller/cmd/main.go

# Use distroless as minimal base image to package the manager binary
# Refer to https://github.com/GoogleContainerTools/distroless for more details
FROM --platform=$BUILDPLATFORM $DOCKER_REGISTRY_GOOGLE/distroless/static:nonroot
WORKDIR /
COPY --from=builder /workspace/manager .
USER 65532:65532

LABEL org.opencontainers.image.source=https://github.com/kagent-dev/kagent
LABEL org.opencontainers.image.description="Kagent controller is the controller for the Kagent app."

ENTRYPOINT ["/manager"]
