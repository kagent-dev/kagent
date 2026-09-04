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
    go build -trimpath -o /kagent-claude ./harness/claude/cmd

FROM alpine:3.22
ARG CLAUDE_CODE_VERSION=2.1.260
ARG CLAUDE_CODE_AMD64_SHA256=08a7ca9c07d88af26d3ea53ed59a27bf74d8827b2f3e01d329f1fd2ae42ceaaa
ARG CLAUDE_CODE_ARM64_SHA256=414fea0ef7bf774b7b1d1a42635ce176b6fc5ec80912303557307f1e04aca621
ARG TARGETARCH
ARG VERSION

ENV USE_BUILTIN_RIPGREP=0
RUN apk add --no-cache bash ca-certificates git libgcc libstdc++ ripgrep \
    && case "${TARGETARCH}" in \
        amd64) platform="linux-x64-musl"; checksum="${CLAUDE_CODE_AMD64_SHA256}" ;; \
        arm64) platform="linux-arm64-musl"; checksum="${CLAUDE_CODE_ARM64_SHA256}" ;; \
        *) echo "unsupported target architecture: ${TARGETARCH}" >&2; exit 1 ;; \
    esac \
    && wget -T 60 -t 3 -O /usr/local/bin/claude \
        "https://downloads.claude.ai/claude-code-releases/${CLAUDE_CODE_VERSION}/${platform}/claude" \
    && echo "${checksum}  /usr/local/bin/claude" | sha256sum -c - \
    && chmod 0755 /usr/local/bin/claude \
    && addgroup -S -g 65532 kagent \
    && adduser -S -D -H -u 65532 -G kagent -h /data -s /sbin/nologin kagent \
    && mkdir -p /data/adapter /data/claude /data/generated /data/workspace \
    && chown -R 65532:65532 /data

COPY --from=builder /kagent-claude /usr/local/bin/kagent-claude

USER 65532:65532
WORKDIR /data/workspace
EXPOSE 80 8081

LABEL org.opencontainers.image.source="https://github.com/kagent-dev/kagent"
LABEL org.opencontainers.image.description="Kagent Claude native Harness adapter"
LABEL org.opencontainers.image.version="$VERSION"

ENTRYPOINT ["/usr/local/bin/kagent-claude"]
