# Build from the go/ directory: docker build -f harness/byo/dsh/Dockerfile .
FROM --platform=$BUILDPLATFORM golang:1.27.0-bookworm@sha256:ded31c68586d2e49e760acc2e65a884b23d032e9bbbed0ae0c55abd3fcaf4452 AS builder
ARG TARGETOS
ARG TARGETARCH
WORKDIR /workspace
COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod go mod download
COPY api/ api/
COPY adk/ adk/
COPY core/ core/
COPY harness/ harness/
COPY pkg/ pkg/
RUN --mount=type=cache,target=/go/pkg/mod --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 GOOS=${TARGETOS:-linux} GOARCH=${TARGETARCH} \
    go build -trimpath -o /kagent-dsh-byo ./harness/byo/dsh/cmd

FROM node:24-bookworm-slim@sha256:d6aa754f16b3197301076f047b5def2f02ea1dbbc2ca920407d46d7ec7f87b20
RUN apt-get update && apt-get install -y --no-install-recommends \
    bash bubblewrap ca-certificates git ripgrep \
    && rm -rf /var/lib/apt/lists/*
WORKDIR /opt/dsh
# Commit the npm lockfile with this example; npm ci checks package integrity.
COPY harness/byo/dsh/package.json harness/byo/dsh/package-lock.json ./
RUN npm ci --omit=dev --ignore-scripts --no-audit --no-fund \
    && ln -s /opt/dsh/node_modules/.bin/dsh /usr/local/bin/dsh \
    && dsh --version \
    && groupadd --gid 65532 kagent \
    && useradd --uid 65532 --gid 65532 --no-create-home --home-dir /data kagent \
    && mkdir -p /data/adapter /data/dsh/sessions /data/dsh/storages /data/workspace \
    && chown -R 65532:65532 /data
COPY --from=builder /kagent-dsh-byo /usr/local/bin/kagent-dsh-byo
USER 65532:65532
WORKDIR /data/workspace
EXPOSE 80 8081
LABEL org.opencontainers.image.source="https://github.com/kagent-dev/kagent"
LABEL org.opencontainers.image.description="DeepSeek Harness reference integration for kagent BYO"
ENTRYPOINT ["/usr/local/bin/kagent-dsh-byo"]
