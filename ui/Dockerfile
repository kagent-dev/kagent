### STAGE 1: Dependencies
ARG BASE_IMAGE_REGISTRY=cgr.dev
ARG TOOLS_NODE_VERSION=24
ARG BUILDPLATFORM
FROM --platform=$BUILDPLATFORM $BASE_IMAGE_REGISTRY/chainguard/wolfi-base:latest AS deps
ENV LANG=C.UTF-8
ENV LC_ALL=C.UTF-8
# This is used to print the build platform in the logs
ARG BUILDPLATFORM
ARG TOOLS_NODE_VERSION

# Tools hold a major version so the bundle always builds with Node 24, and old revisions can't break the build.
RUN echo "Installing on $BUILDPLATFORM" \
    && apk add --no-cache \
        "curl~8" \
        "bash~5" \
        "openssl~3" \
        "unzip~6" \
        ca-certificates \
        "nodejs-${TOOLS_NODE_VERSION}" \
        npm-12 \
        "node-gyp~13" \
    && update-ca-certificates

ENV DO_NOT_TRACK=1

WORKDIR /app/ui

# Yarn 4, pinned to package.json's "packageManager" so the image and a developer
# machine resolve the lockfile identically. Corepack is the supported way to get
# it, but wolfi's nodejs package does not bundle corepack, so it comes from npm
# first. `--immutable` fails the build if yarn.lock would have to change.
COPY package.json yarn.lock .yarnrc.yml ./
RUN --mount=type=cache,target=/root/.npm,rw \
    YARN_VERSION="$(sed -n 's/.*"packageManager"[[:space:]]*:[[:space:]]*"yarn@\([0-9.]*\)".*/\1/p' package.json)" \
    && npm install -g corepack@0.36.0 \
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

### STAGE 3: Runtime
# Distroless nginx: no shell or package manager. The chart mounts nginx.conf and
# env-config.js, so the image carries only the static bundle.
FROM $BASE_IMAGE_REGISTRY/chainguard/nginx:latest AS final

COPY --from=builder /app/ui/dist /usr/share/nginx/html

EXPOSE 8080
ARG VERSION

LABEL org.opencontainers.image.source=https://github.com/kagent-dev/kagent
LABEL org.opencontainers.image.description="Kagent app is the UI and apiserver for running agents."
LABEL org.opencontainers.image.authors="Kagent Creators 🤖"
LABEL org.opencontainers.image.version="$VERSION"

USER 1001
