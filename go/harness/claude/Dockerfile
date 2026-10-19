ARG BASE_IMAGE_REGISTRY=cgr.dev
ARG BUILDPLATFORM
FROM --platform=$BUILDPLATFORM ${BASE_IMAGE_REGISTRY}/chainguard/go:latest AS builder
ARG TARGETARCH
ARG TARGETOS

WORKDIR /workspace
COPY go.mod go.sum ./
# go mod download retries a transient proxy error; a missing module still fails on the third attempt.
RUN --mount=type=cache,target=/root/go/pkg/mod,rw \
    --mount=type=cache,target=/root/.cache/go-build,rw \
    for attempt in 1 2 3; do \
        go mod download && break; \
        status=$?; [ "$attempt" -lt 3 ] || exit "$status"; \
        echo "go mod download failed (attempt $attempt of 3), retrying in 5s" >&2; sleep 5; \
    done
COPY api/ api/
COPY adk/ adk/
COPY core/ core/
COPY harness/ harness/
COPY pkg/ pkg/
RUN --mount=type=cache,target=/root/go/pkg/mod,rw \
    --mount=type=cache,target=/root/.cache/go-build,rw \
    CGO_ENABLED=0 GOOS=${TARGETOS:-linux} GOARCH=${TARGETARCH} \
    go build -trimpath -o /kagent-claude ./harness/claude/cmd

FROM alpine:3.24
ARG CLAUDE_CODE_VERSION=2.1.303
ARG CLAUDE_CODE_AMD64_SHA256=8dfd02ad4590c6f4c419ec73af4f5462223a07f64495e2c2fce45990faf1ede6
ARG CLAUDE_CODE_ARM64_SHA256=a0d9d4dcb99a1782cdfc332fed8510467e99efc7ff2f3d7443fea27d9af55e7e
ARG TARGETARCH
ARG VERSION

ENV USE_BUILTIN_RIPGREP=0
# The GitHub CLI from its release, as go/Dockerfile installs it (github-cli.version,
# install-github-cli.sh). The GitLab CLI at gitlab-cli.version, in this image
# only: glab needs a writable home for its config, and the harness gives
# Claude's user one. curl shows an HTTP error's status line and body, which
# WebFetch drops; managed-CLAUDE.md steers Claude to it.
# apk retries a transient mirror error; a missing package still fails on the third attempt.
RUN --mount=type=bind,source=github-cli.version,target=/tmp/github-cli.version \
    --mount=type=bind,source=install-github-cli.sh,target=/tmp/install-github-cli.sh \
    --mount=type=bind,source=gitlab-cli.version,target=/tmp/gitlab-cli.version \
    for attempt in 1 2 3; do \
        apk upgrade --no-cache && apk add --no-cache bash ca-certificates git curl libgcc libstdc++ ripgrep \
            "glab~$(cat /tmp/gitlab-cli.version)" && break; \
        status=$?; [ "$attempt" -lt 3 ] || exit "$status"; \
        echo "apk failed (attempt $attempt of 3), retrying in 5s" >&2; sleep 5; \
    done \
    && sh /tmp/install-github-cli.sh /tmp/github-cli.version "${TARGETARCH}" \
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
# Claude Code reads its managed CLAUDE.md on every start, beside the managed
# settings the harness writes there.
COPY harness/claude/managed-CLAUDE.md /etc/claude-code/CLAUDE.md

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
