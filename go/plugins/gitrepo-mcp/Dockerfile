### STAGE 1: build
ARG BASE_IMAGE_REGISTRY=cgr.dev
ARG BUILDPLATFORM
FROM --platform=$BUILDPLATFORM $BASE_IMAGE_REGISTRY/chainguard/go:latest AS builder
ARG TARGETARCH

WORKDIR /workspace
COPY go.work .
COPY api/go.mod api/go.sum api/
COPY core/go.mod core/go.sum core/
COPY adk/go.mod adk/go.sum adk/
COPY plugins/kanban-mcp/go.mod plugins/kanban-mcp/go.sum plugins/kanban-mcp/
COPY plugins/gitrepo-mcp/go.mod plugins/gitrepo-mcp/go.sum plugins/gitrepo-mcp/
COPY plugins/temporal-mcp/go.mod plugins/temporal-mcp/go.sum plugins/temporal-mcp/
RUN --mount=type=cache,target=/root/go/pkg/mod,rw      \
    --mount=type=cache,target=/root/.cache/go-build,rw \
     go work sync && go mod download
COPY api/ api/
COPY core/ core/
COPY adk/ adk/
COPY plugins/ plugins/

ARG LDFLAGS
RUN --mount=type=cache,target=/root/go/pkg/mod,rw             \
    --mount=type=cache,target=/root/.cache/go-build,rw        \
    echo "Building on $BUILDPLATFORM -> linux/$TARGETARCH" && \
    CGO_ENABLED=0 GOOS=${TARGETOS:-linux} GOARCH=${TARGETARCH} go build -a -ldflags "$LDFLAGS" -o /app "./plugins/gitrepo-mcp/"

### STAGE 2: runtime with git
ARG BASE_IMAGE_REGISTRY=cgr.dev
FROM $BASE_IMAGE_REGISTRY/chainguard/wolfi-base:latest
RUN apk add --no-cache git ca-certificates
COPY --from=builder /app /app
USER 65532:65532

ARG VERSION
LABEL org.opencontainers.image.source=https://github.com/kagent-dev/kagent
LABEL org.opencontainers.image.description="Kagent gitrepo-mcp plugin"
LABEL org.opencontainers.image.version="$VERSION"

ENTRYPOINT ["/app"]
