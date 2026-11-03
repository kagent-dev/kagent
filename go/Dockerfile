### STAGE 1: base image
ARG BASE_IMAGE_REGISTRY=cgr.dev
ARG BUILDPLATFORM
ARG NODE_IMAGE=node:24.22.1-alpine@sha256:06d7adf3e0ef61a13f1d28682726366f97a15aa1888c934d3ecec86c33918b53
FROM --platform=$BUILDPLATFORM $BASE_IMAGE_REGISTRY/chainguard/go:latest AS builder
ARG TARGETARCH
ARG TARGETPLATFORM
ARG BUILDPLATFORM

# Which package to build (e.g. core/cmd/controller/main.go, adk/cmd/main.go)
ARG BUILD_PACKAGE=core/cmd/controller/main.go

WORKDIR /workspace
# Copy the Go module manifests
COPY go.mod go.sum .
# Copy all source
COPY api/ api/
COPY core/ core/
COPY adk/ adk/
COPY harness/ harness/
COPY pkg/ pkg/

# Build
ARG LDFLAGS
RUN --mount=type=cache,target=/root/go/pkg/mod,rw             \
    --mount=type=cache,target=/root/.cache/go-build,rw        \
    echo "Building on $BUILDPLATFORM -> linux/$TARGETARCH" && \
    CGO_ENABLED=0 GOOS=${TARGETOS:-linux} GOARCH=${TARGETARCH} go build -ldflags "$LDFLAGS" -o /app "$BUILD_PACKAGE"

### STAGE 2: final image
FROM alpine:3.24 AS runtime
ARG TARGETPLATFORM

# The runtime user gets a passwd entry and a home it owns: commands an agent
# runs (git config --global, CLIs that keep a config under $HOME/.config)
# write there.
RUN apk upgrade --no-cache && apk add --no-cache bash ca-certificates git && \
    addgroup -S -g 65532 kagent && \
    adduser -S -D -u 65532 -G kagent -h /home/kagent kagent && \
    mkdir -p /config /skills /plugins /data/plugins && \
    chown -R 65532:65532 /config /skills /plugins /data

WORKDIR /
COPY --from=builder /app /app
USER 65532:65532
ENV HOME=/home/kagent
ARG VERSION

LABEL org.opencontainers.image.source=https://github.com/kagent-dev/kagent
LABEL org.opencontainers.image.description="Kagent app is the UI and apiserver for running agents."
LABEL org.opencontainers.image.version="$VERSION"

ENTRYPOINT ["/app"]

### Controller only: the UI bundle, from the `ui` build context (--build-context ui=./ui).
FROM --platform=$BUILDPLATFORM $NODE_IMAGE AS ui-builder
ENV DO_NOT_TRACK=1
WORKDIR /app/ui
# Yarn comes from package.json's "packageManager"; --immutable fails if yarn.lock would change.
COPY --from=ui package.json yarn.lock .yarnrc.yml ./
RUN YARN_VERSION="$(sed -n 's/.*"packageManager"[[:space:]]*:[[:space:]]*"yarn@\([0-9.]*\)".*/\1/p' package.json)" \
    && corepack enable \
    && corepack prepare "yarn@${YARN_VERSION}" --activate \
    && yarn install --immutable
COPY --from=ui . .
# The mock service worker is a dev and e2e fixture, so it never reaches the image.
RUN yarn build && rm -f dist/mockServiceWorker.js

# The controller needs no shell, git or libc: the binary is static and serves the UI itself.
FROM $BASE_IMAGE_REGISTRY/chainguard/static AS controller
ARG VERSION
COPY --from=builder /app /app
COPY --from=ui-builder /app/ui/dist /usr/share/kagent/ui
USER 65532:65532

LABEL org.opencontainers.image.source=https://github.com/kagent-dev/kagent
LABEL org.opencontainers.image.description="Kagent app is the UI and apiserver for running agents."
LABEL org.opencontainers.image.version="$VERSION"

ENTRYPOINT ["/app"]

# The default target stays the plain runtime for every other BUILD_PACKAGE.
FROM runtime
