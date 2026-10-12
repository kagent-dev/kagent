### STAGE 1: base image
ARG BASE_IMAGE_REGISTRY=cgr.dev
ARG BUILDPLATFORM
FROM --platform=$BUILDPLATFORM $BASE_IMAGE_REGISTRY/chainguard/go:latest AS builder
ARG TARGETARCH
ARG TARGETPLATFORM
ARG BUILDPLATFORM

# Which package to build (e.g. core/cmd/controller/main.go, adk/cmd/main.go)
ARG BUILD_PACKAGE=core/cmd/controller/main.go

WORKDIR /workspace
# Copy the Go module manifests
COPY go.mod go.sum .
# cache deps before building and copying source so that we don't need to re-download as much
# and so that source changes don't invalidate our downloaded layer
# go mod download retries a transient proxy error; a missing module still fails on the third attempt.
RUN --mount=type=cache,target=/root/go/pkg/mod,rw      \
    --mount=type=cache,target=/root/.cache/go-build,rw \
    for attempt in 1 2 3; do \
        go mod download && break; \
        status=$?; [ "$attempt" -lt 3 ] || exit "$status"; \
        echo "go mod download failed (attempt $attempt of 3), retrying in 5s" >&2; sleep 5; \
    done
# Copy all source
COPY api/ api/
COPY core/ core/
COPY adk/ adk/
COPY harness/ harness/
COPY pkg/ pkg/

# Build
ARG LDFLAGS
RUN --mount=type=cache,target=/root/go/pkg/mod,rw             \
    --mount=type=cache,target=/root/.cache/go-build,rw        \
    echo "Building on $BUILDPLATFORM -> linux/$TARGETARCH" && \
    CGO_ENABLED=0 GOOS=${TARGETOS:-linux} GOARCH=${TARGETARCH} go build -ldflags "$LDFLAGS" -o /app "$BUILD_PACKAGE"

### STAGE 2: final image
FROM alpine:3.24
ARG TARGETPLATFORM

# The GitHub CLI for an agent's work in its clone. github-cli.version pins it
# once for this image and the Claude harness image; apk's ~ takes any Alpine
# rebuild (-rN) of that version. The runtime user gets a passwd entry and a
# home it owns: commands an agent runs (git config --global, CLIs that keep a
# config under $HOME/.config) write there.
# apk retries a transient mirror error; a missing package still fails on the third attempt.
RUN --mount=type=bind,source=github-cli.version,target=/tmp/github-cli.version \
    for attempt in 1 2 3; do \
        apk upgrade --no-cache && \
        apk add --no-cache bash ca-certificates git "github-cli~$(cat /tmp/github-cli.version)" && break; \
        status=$?; [ "$attempt" -lt 3 ] || exit "$status"; \
        echo "apk failed (attempt $attempt of 3), retrying in 5s" >&2; sleep 5; \
    done && \
    addgroup -S -g 65532 kagent && \
    adduser -S -D -u 65532 -G kagent -h /home/kagent kagent && \
    mkdir -p /config /skills /plugins /data/plugins && \
    chown -R 65532:65532 /config /skills /plugins /data

WORKDIR /
COPY --from=builder /app /app
USER 65532:65532
ENV HOME=/home/kagent
ARG VERSION

LABEL org.opencontainers.image.source=https://github.com/kagent-dev/kagent
LABEL org.opencontainers.image.description="Kagent app is the UI and apiserver for running agents."
LABEL org.opencontainers.image.version="$VERSION"

ENTRYPOINT ["/app"]
