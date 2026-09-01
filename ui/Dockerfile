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

RUN echo "Installing on $BUILDPLATFORM" \
    && apk add --no-cache curl bash openssl unzip ca-certificates "nodejs~${TOOLS_NODE_VERSION}" npm node-gyp \
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
    && npm install -g corepack \
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
# nginx only — the UI is a static bundle now, so there is no node process and no
# node runtime in the final image.
FROM $BASE_IMAGE_REGISTRY/chainguard/wolfi-base:latest AS final
ENV LANG=C.UTF-8
ENV LC_ALL=C.UTF-8
# This is used to print the build platform in the logs
ARG BUILDPLATFORM

RUN echo "Installing on $BUILDPLATFORM" \
    && apk add --no-cache curl bash openssl ca-certificates nginx \
    && update-ca-certificates

# `kagent` owns both the static root and the writable runtime dirs. /tmp/kagent
# holds env-config.js, which init.sh renders from env on every start — it lives
# under /tmp because the container is expected to run with a read-only root
# filesystem, where /tmp is the mounted emptyDir.
RUN addgroup -g 1001 kagent                          \
    && adduser -u 1001 -G kagent -s /bin/bash -D kagent \
    && mkdir -p /usr/share/nginx/html                \
                /tmp/kagent                          \
                /tmp/nginx/client_temp               \
                /tmp/nginx/proxy_temp                \
                /tmp/nginx/fastcgi_temp              \
                /tmp/nginx/uwsgi_temp                \
                /tmp/nginx/scgi_temp                 \
    && chown -R kagent:kagent /usr/share/nginx/html /tmp/kagent /tmp/nginx \
    # nginx opens its compiled-in default error log before it parses the config
    # that redirects logging to stderr. On a read-only root filesystem that open
    # fails and every start logs an alert, so point the default at stderr too.
    && mkdir -p /var/lib/nginx/logs \
    && ln -sf /dev/stderr /var/lib/nginx/logs/error.log

COPY scripts/init.sh /usr/local/bin/init.sh
COPY --from=builder --chown=kagent:kagent /app/ui/dist /usr/share/nginx/html

RUN chmod +x /usr/local/bin/init.sh

EXPOSE 8080
ARG VERSION

LABEL org.opencontainers.image.source=https://github.com/kagent-dev/kagent
LABEL org.opencontainers.image.description="Kagent app is the UI and apiserver for running agents."
LABEL org.opencontainers.image.authors="Kagent Creators 🤖"
LABEL org.opencontainers.image.version="$VERSION"

USER 1001

CMD ["/usr/local/bin/init.sh"]
