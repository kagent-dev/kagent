### STAGE 1: Dependencies
ARG BASE_IMAGE_REGISTRY=cgr.dev
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

### STAGE 3: Server
# A static binary, so the runtime image needs no shell or libc.
FROM --platform=$BUILDPLATFORM $GO_IMAGE AS server
ARG TARGETARCH
WORKDIR /src
COPY entrypoint/ .
RUN CGO_ENABLED=0 GOOS=linux GOARCH=$TARGETARCH go build -trimpath -ldflags "-s -w" -o /server .

### STAGE 4: Runtime
# The server renders env-config.js from env into memory on every start, so it
# runs under a read-only root filesystem.
FROM $BASE_IMAGE_REGISTRY/chainguard/static AS final

COPY --from=server /server /usr/local/bin/server
COPY --from=builder /app/ui/dist /app/html

EXPOSE 8080
ARG VERSION

LABEL org.opencontainers.image.source=https://github.com/kagent-dev/kagent
LABEL org.opencontainers.image.description="Kagent app is the UI and apiserver for running agents."
LABEL org.opencontainers.image.authors="Kagent Creators 🤖"
LABEL org.opencontainers.image.version="$VERSION"

USER 1001

ENTRYPOINT ["/usr/local/bin/server"]
CMD []
