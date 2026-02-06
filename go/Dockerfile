### STAGE 1: base image
ARG BASE_IMAGE_REGISTRY=docker.io
ARG BUILDPLATFORM
# Use golang image which includes gcc for CGO builds (required for sqlite-vec)
FROM --platform=$BUILDPLATFORM golang:1.25-bookworm AS builder
ARG TARGETARCH
ARG TARGETPLATFORM
# This is used to print the build platform in the logs
ARG BUILDPLATFORM

WORKDIR /workspace

# Install C development headers for SQLite (required for sqlite-vec CGO build)
RUN apt-get update && apt-get install -y libsqlite3-dev && rm -rf /var/lib/apt/lists/*

# Copy the Go Modules manifests
COPY go.mod go.mod
COPY go.sum go.sum
# cache deps before building and copying source so that we don't need to re-download as much
# and so that source changes don't invalidate our downloaded layer
RUN --mount=type=cache,target=/root/go/pkg/mod,rw      \
    --mount=type=cache,target=/root/.cache/go-build,rw \
     go mod download

# Copy the go source
COPY api api
COPY cmd cmd
COPY pkg pkg
COPY internal internal
# Build
# the GOARCH has not a default value to allow the binary be built according to the host where the command
# was called. For example, if we call make docker-build in a local env which has the Apple Silicon M1 SO
# the docker BUILDPLATFORM arg will be linux/arm64 when for Apple x86 it will be linux/amd64. Therefore,
# by leaving it empty we can ensure that the container and binary shipped on it will have the same platform.
ARG LDFLAGS
RUN --mount=type=cache,target=/root/go/pkg/mod,rw             \
    --mount=type=cache,target=/root/.cache/go-build,rw        \
    echo "Building on $BUILDPLATFORM -> linux/$TARGETARCH" && \
    CGO_ENABLED=1 GOOS=${TARGETOS:-linux} GOARCH=${TARGETARCH} go build -a -ldflags "$LDFLAGS" -o manager cmd/controller/main.go

### STAGE 2: final image
# Use distroless/cc-debian12 which includes C/C++ runtime libraries
# Required for CGO-compiled sqlite-vec extension
# Refer to https://github.com/GoogleContainerTools/distroless for more details
FROM gcr.io/distroless/cc-debian12:nonroot
ARG TARGETPLATFORM

WORKDIR /
COPY --from=builder /workspace/manager /manager
USER 65532:65532
ARG VERSION

LABEL org.opencontainers.image.source=https://github.com/kagent-dev/kagent
LABEL org.opencontainers.image.description="Kagent app is the UI and apiserver for running agents."
LABEL org.opencontainers.image.authors="Kagent Creators 🤖"
LABEL org.opencontainers.image.version="$VERSION"

ENTRYPOINT ["/manager"]