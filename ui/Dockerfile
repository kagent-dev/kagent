### STAGE 1: Dependencies
ARG BASE_IMAGE_REGISTRY=cgr.dev
ARG NGINX_DIGEST=sha256:8a42fb9468e353a6c2501d2657866333b7a2a075912f4a7e4fa73298aafd46d6
ARG GO_IMAGE=golang:1.27.3-alpine@sha256:b8c9b82affbbb82a020bbcf998dad2ba9516944ac7e883524642a481a9505e9c
ARG NODE_IMAGE=node:24.22.1-alpine@sha256:06d7adf3e0ef61a13f1d28682726366f97a15aa1888c934d3ecec86c33918b53
ARG BUILDPLATFORM
FROM --platform=$BUILDPLATFORM $NODE_IMAGE AS deps

ENV DO_NOT_TRACK=1

WORKDIR /app/ui

# Yarn 4, pinned to package.json's "packageManager" so the image and a developer
# machine resolve the lockfile identically. Corepack, bundled with the Node image,
# fetches it. `--immutable` fails the build if yarn.lock would have to change.
COPY package.json yarn.lock .yarnrc.yml ./
RUN YARN_VERSION="$(sed -n 's/.*"packageManager"[[:space:]]*:[[:space:]]*"yarn@\([0-9.]*\)".*/\1/p' package.json)" \
    && corepack enable \
    && corepack prepare "yarn@${YARN_VERSION}" --activate \
    && yarn install --immutable

### STAGE 2: Build
FROM --platform=$BUILDPLATFORM deps AS builder

# Copy source files
COPY . .

# Produces a static bundle in dist/. `yarn build` type-checks first, so a type
# error fails the image build rather than shipping.
#
# The mock service worker is a development and e2e fixture. It is dropped here
# rather than in the runtime COPY so it cannot reach the image at all: a worker
# sitting at the production web root is one build-mode mistake away from being
# registered and serving fixtures to real users. It stays in the repo, where dev
# and Playwright need it.
RUN yarn build \
    && rm -f dist/mockServiceWorker.js

### STAGE 3: Entrypoint
# A static binary, so the runtime image needs no shell to render settings at start.
FROM --platform=$BUILDPLATFORM $GO_IMAGE AS entrypoint
ARG TARGETARCH
WORKDIR /src
COPY entrypoint/ .
RUN CGO_ENABLED=0 GOOS=linux GOARCH=$TARGETARCH go build -trimpath -ldflags "-s -w" -o /entrypoint . \
    # nginx opens its compiled-in error log before reading the config; on a read-only root that logs an alert.
    && mkdir -p /logs && ln -s /dev/stderr /logs/error.log

### STAGE 4: Runtime
# Distroless nginx: no shell or package manager. The entrypoint renders
# env-config.js from env on every start into /tmp, the writable emptyDir under a
# read-only root filesystem, then execs nginx.
FROM $BASE_IMAGE_REGISTRY/chainguard/nginx:latest@${NGINX_DIGEST} AS final

COPY --from=entrypoint /entrypoint /usr/local/bin/entrypoint
COPY --from=entrypoint /logs/ /var/lib/nginx/logs/
COPY --from=builder /app/ui/dist /usr/share/nginx/html

EXPOSE 8080
ARG VERSION

LABEL org.opencontainers.image.source=https://github.com/kagent-dev/kagent
LABEL org.opencontainers.image.description="Kagent app is the UI and apiserver for running agents."
LABEL org.opencontainers.image.authors="Kagent Creators 🤖"
LABEL org.opencontainers.image.version="$VERSION"

USER 1001

ENTRYPOINT ["/usr/local/bin/entrypoint"]
CMD []
