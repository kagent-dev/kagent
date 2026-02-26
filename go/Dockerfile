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
# Copy the Go workspace and all module manifests
COPY go.work .
COPY api/go.mod api/go.sum api/
COPY core/go.mod core/go.sum core/
COPY adk/go.mod adk/go.sum adk/
# cache deps before building and copying source so that we don't need to re-download as much
# and so that source changes don't invalidate our downloaded layer
RUN --mount=type=cache,target=/root/go/pkg/mod,rw      \
    --mount=type=cache,target=/root/.cache/go-build,rw \
     go work sync && go mod download
# Copy all source
COPY api/ api/
COPY core/ core/
COPY adk/ adk/

# Build
ARG LDFLAGS
RUN --mount=type=cache,target=/root/go/pkg/mod,rw             \
    --mount=type=cache,target=/root/.cache/go-build,rw        \
    echo "Building on $BUILDPLATFORM -> linux/$TARGETARCH" && \
    CGO_ENABLED=0 GOOS=${TARGETOS:-linux} GOARCH=${TARGETARCH} go build -a -ldflags "$LDFLAGS" -o /app "$BUILD_PACKAGE"

### STAGE 2: final image
FROM gcr.io/distroless/static:nonroot
ARG TARGETPLATFORM

WORKDIR /
COPY --from=builder /app /app
USER 65532:65532
ARG VERSION

LABEL org.opencontainers.image.source=https://github.com/kagent-dev/kagent
LABEL org.opencontainers.image.description="Kagent app is the UI and apiserver for running agents."
LABEL org.opencontainers.image.version="$VERSION"

ENTRYPOINT ["/app"]
