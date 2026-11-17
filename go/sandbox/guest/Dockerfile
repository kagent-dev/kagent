ARG BASE_IMAGE_REGISTRY=cgr.dev
ARG ALPINE_VERSION=3.24
ARG BUILDPLATFORM
FROM --platform=$BUILDPLATFORM $BASE_IMAGE_REGISTRY/chainguard/go:latest AS builder
ARG TARGETARCH

WORKDIR /workspace
COPY go.mod go.sum ./
COPY core/pkg/consts/ core/pkg/consts/
COPY core/pkg/env/ core/pkg/env/
COPY pkg/logging/ pkg/logging/
COPY sandbox/guest/cmd/ sandbox/guest/cmd/

RUN --mount=type=cache,target=/root/go/pkg/mod,rw \
    --mount=type=cache,target=/root/.cache/go-build,rw \
    CGO_ENABLED=0 GOOS=linux GOARCH=$TARGETARCH \
    go build -mod=readonly -trimpath -o /kagent-sandbox-guest ./sandbox/guest/cmd

FROM --platform=$BUILDPLATFORM alpine:${ALPINE_VERSION} AS anthropic-cli
ARG TARGETARCH
ARG ANT_VERSION=2.5.0

WORKDIR /download
RUN set -eux; \
    case "$TARGETARCH" in amd64|arm64) ;; *) exit 1 ;; esac; \
    archive="ant_${ANT_VERSION}_linux_${TARGETARCH}.tar.gz"; \
    release="https://github.com/anthropics/anthropic-cli/releases/download/v${ANT_VERSION}"; \
    wget -q "$release/$archive"; \
    wget -q -O checksums.txt "$release/ant_${ANT_VERSION}_checksums.txt"; \
    grep "  ${archive}$" checksums.txt | sha256sum -c -; \
    mkdir /out; \
    tar -xzf "$archive" -C /out ant; \
    chmod 755 /out/ant

FROM alpine:${ALPINE_VERSION}

RUN apk upgrade --no-cache && apk add --no-cache bash ca-certificates git && \
    mkdir -p /data/workspace /data/guest-logs /home/nonroot && \
    chown -R 65532:65532 /data /home/nonroot

ENV HOME=/home/nonroot

COPY --from=anthropic-cli /out/ant /usr/local/bin/ant
COPY --from=builder /kagent-sandbox-guest /usr/local/bin/kagent-sandbox-guest
# The guest creates its workspace after the durable /data volume is mounted.
WORKDIR /data
USER 65532:65532
EXPOSE 80

ARG VERSION
LABEL org.opencontainers.image.source=https://github.com/kagent-dev/kagent
LABEL org.opencontainers.image.description="Process and file guest for standalone kagent sandboxes."
LABEL org.opencontainers.image.version="$VERSION"

ENTRYPOINT ["/usr/local/bin/kagent-sandbox-guest"]
CMD ["--listen=:80", "--workspace=/data/workspace", "--log-dir=/data/guest-logs"]
