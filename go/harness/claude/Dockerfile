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
ARG CLAUDE_CODE_VERSION=2.1.285
ARG CLAUDE_CODE_AMD64_SHA256=7b4414af1bc06eb6d91730759bd01c4c02a4976d0b8526237e9a6d8c33eaf102
ARG CLAUDE_CODE_ARM64_SHA256=31efc4136bc678575f4c6730e248d34f89dbfea0468be1c5d012af199cd62ee8
ARG TARGETARCH
ARG VERSION

ENV USE_BUILTIN_RIPGREP=0
RUN apk upgrade --no-cache && apk add --no-cache bash ca-certificates git libgcc libstdc++ ripgrep \
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

# The harness runs as root and starts Claude Code as the kagent user, so that
# Claude and its tools cannot read the harness process or change the policy it
# writes. The harness sets Claude's working directory itself.
USER 0:0
WORKDIR /
EXPOSE 80 8081

LABEL org.opencontainers.image.source="https://github.com/kagent-dev/kagent"
LABEL org.opencontainers.image.description="Kagent Claude native Harness adapter"
LABEL org.opencontainers.image.version="$VERSION"

ENTRYPOINT ["/usr/local/bin/kagent-claude"]
