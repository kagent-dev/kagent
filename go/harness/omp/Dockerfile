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
    go build -trimpath -o /kagent-omp ./harness/omp/cmd

FROM alpine:3.22
ARG OMP_VERSION=18.11.0
ARG OMP_AMD64_SHA256=a524103ca0cf4020fa7770c9316b11c5fee447968e4f6f2f740d6beb5df8d030
ARG OMP_ARM64_SHA256=42b16ea9209774f666bc84283a7d733c7ac63fc241800dcab991ccdf64657ad7
ARG TARGETARCH
ARG VERSION

# apk retries a transient mirror error; a missing package still fails on the third attempt.
RUN for attempt in 1 2 3; do \
        apk upgrade --no-cache && apk add --no-cache bash ca-certificates git libgcc libstdc++ && break; \
        status=$?; [ "$attempt" -lt 3 ] || exit "$status"; \
        echo "apk failed (attempt $attempt of 3), retrying in 5s" >&2; sleep 5; \
    done \
    && case "${TARGETARCH}" in \
        amd64) platform="x64"; checksum="${OMP_AMD64_SHA256}" ;; \
        arm64) platform="arm64"; checksum="${OMP_ARM64_SHA256}" ;; \
        *) echo "unsupported target architecture: ${TARGETARCH}" >&2; exit 1 ;; \
    esac \
    && wget -T 60 -t 3 -O /usr/local/bin/omp \
        "https://github.com/can1357/oh-my-pi/releases/download/v${OMP_VERSION}/omp-linux-musl-${platform}" \
    && echo "${checksum}  /usr/local/bin/omp" | sha256sum -c - \
    && chmod 0755 /usr/local/bin/omp \
    && addgroup -S -g 65532 kagent \
    && adduser -S -D -H -u 65532 -G kagent -h /data -s /sbin/nologin kagent \
    && mkdir -p /data/adapter /data/omp /data/workspace \
    && chown -R 65532:65532 /data

COPY --from=builder /kagent-omp /usr/local/bin/kagent-omp

USER 65532:65532
ENV HOME=/data
# The harness sets omp's working directory itself; /data is a volume that is
# empty on first start.
WORKDIR /
EXPOSE 80

LABEL org.opencontainers.image.source="https://github.com/giantswarm/kagent-upstream"
LABEL org.opencontainers.image.description="Kagent oh-my-pi (omp) BYO Harness adapter"
LABEL org.opencontainers.image.version="$VERSION"

ENTRYPOINT ["/usr/local/bin/kagent-omp"]
