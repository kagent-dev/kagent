# Build from go/: docker build -f harness/byo/dsh/approval-mcp/Dockerfile .
FROM --platform=$BUILDPLATFORM golang:1.27.0-bookworm@sha256:ded31c68586d2e49e760acc2e65a884b23d032e9bbbed0ae0c55abd3fcaf4452 AS builder
ARG TARGETOS
ARG TARGETARCH
WORKDIR /workspace
COPY go.mod go.sum ./
COPY harness/byo/dsh/internal/operations/ harness/byo/dsh/internal/operations/
COPY harness/byo/dsh/cmd/approval-mcp/ harness/byo/dsh/cmd/approval-mcp/
COPY pkg/logging/ pkg/logging/
RUN --mount=type=cache,target=/go/pkg/mod --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 GOOS=${TARGETOS:-linux} GOARCH=${TARGETARCH} \
    go build -trimpath -o /approval-mcp ./harness/byo/dsh/cmd/approval-mcp

FROM scratch
COPY --from=builder /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/ca-certificates.crt
COPY --from=builder /approval-mcp /approval-mcp
USER 65532:65532
EXPOSE 8080
ENTRYPOINT ["/approval-mcp"]
