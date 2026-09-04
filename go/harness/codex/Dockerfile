ARG BASE_IMAGE_REGISTRY=cgr.dev
ARG BUILDPLATFORM
FROM --platform=$BUILDPLATFORM ${BASE_IMAGE_REGISTRY}/chainguard/go:latest AS builder
ARG TARGETARCH
ARG TARGETOS

WORKDIR /workspace
COPY go.mod go.sum ./
RUN --mount=type=cache,target=/root/go/pkg/mod,rw \
    --mount=type=cache,target=/root/.cache/go-build,rw \
    go mod download
COPY api/ api/
COPY adk/ adk/
COPY core/ core/
COPY harness/ harness/
COPY pkg/ pkg/
RUN --mount=type=cache,target=/root/go/pkg/mod,rw \
    --mount=type=cache,target=/root/.cache/go-build,rw \
    CGO_ENABLED=0 GOOS=${TARGETOS:-linux} GOARCH=${TARGETARCH} \
    go build -trimpath -o /kagent-codex ./harness/codex/cmd

FROM alpine:3.22
ARG CODEX_CLI_VERSION=0.148.0
ARG CODEX_CLI_AMD64_SHA256=8c790500af2ba6e74ce4948fe26c651ac1f77f6dbb005b47c8d26ff711146262
ARG CODEX_CLI_ARM64_SHA256=580db3c7411f5852b550876f185c30b61b674e01b948fd5030f2cd7a30db110a
ARG TARGETARCH
ARG VERSION

RUN apk add --no-cache bash ca-certificates git libgcc libstdc++ \
    && case "${TARGETARCH}" in \
        amd64) platform="x86_64-unknown-linux-musl"; checksum="${CODEX_CLI_AMD64_SHA256}" ;; \
        arm64) platform="aarch64-unknown-linux-musl"; checksum="${CODEX_CLI_ARM64_SHA256}" ;; \
        *) echo "unsupported target architecture: ${TARGETARCH}" >&2; exit 1 ;; \
    esac \
    && wget -T 60 -t 3 -O /tmp/codex.tar.gz \
        "https://github.com/openai/codex/releases/download/rust-v${CODEX_CLI_VERSION}/codex-package-${platform}.tar.gz" \
    && echo "${checksum}  /tmp/codex.tar.gz" | sha256sum -c - \
    && tar -xzf /tmp/codex.tar.gz -C /usr/local \
    && rm /tmp/codex.tar.gz \
    && addgroup -S -g 65532 kagent \
    && adduser -S -D -H -u 65532 -G kagent -h /data -s /sbin/nologin kagent \
    && mkdir -p /data/adapter /data/codex /data/workspace \
    && chown -R 65532:65532 /data

COPY --from=builder /kagent-codex /usr/local/bin/kagent-codex

USER 65532:65532
WORKDIR /data/workspace
EXPOSE 80 8081

LABEL org.opencontainers.image.source="https://github.com/kagent-dev/kagent"
LABEL org.opencontainers.image.description="Kagent Codex native Harness adapter"
LABEL org.opencontainers.image.version="$VERSION"

ENTRYPOINT ["/usr/local/bin/kagent-codex"]
