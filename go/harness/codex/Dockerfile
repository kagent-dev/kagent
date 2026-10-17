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
    go build -trimpath -o /kagent-codex ./harness/codex/cmd

FROM alpine:3.22
ARG CODEX_CLI_VERSION=0.163.0
ARG CODEX_CLI_AMD64_SHA256=582646c6bf0561eb0de540f7fc85b9121b0091a826bc6e96830b7f1722122182
ARG CODEX_CLI_ARM64_SHA256=9d8ecf2d19d3e184877983902d6c85de48b3888dcb72c382a8a0c975845927fb
ARG TARGETARCH
ARG VERSION

# apk retries a transient mirror error; a missing package still fails on the third attempt.
RUN for attempt in 1 2 3; do \
        apk upgrade --no-cache && apk add --no-cache bash ca-certificates git libgcc libstdc++ && break; \
        status=$?; [ "$attempt" -lt 3 ] || exit "$status"; \
        echo "apk failed (attempt $attempt of 3), retrying in 5s" >&2; sleep 5; \
    done \
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
# The harness sets Codex's working directory itself; /data is a volume that is
# empty on first start.
WORKDIR /
EXPOSE 80 8081

LABEL org.opencontainers.image.source="https://github.com/kagent-dev/kagent"
LABEL org.opencontainers.image.description="Kagent Codex native Harness adapter"
LABEL org.opencontainers.image.version="$VERSION"

ENTRYPOINT ["/usr/local/bin/kagent-codex"]
