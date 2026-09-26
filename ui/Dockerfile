### STAGE 1: Dependencies
ARG BASE_IMAGE_REGISTRY=cgr.dev
ARG TOOLS_NODE_VERSION=24
ARG BUILDPLATFORM
FROM --platform=$BUILDPLATFORM $BASE_IMAGE_REGISTRY/chainguard/wolfi-base:latest@sha256:08df5982c3d27e70a4ce1607e3bb9af09d746f8722cf135a7694afef879fc5a2 AS deps
ENV LANG=C.UTF-8
ENV LC_ALL=C.UTF-8
# This is used to print the build platform in the logs
ARG BUILDPLATFORM
ARG TOOLS_NODE_VERSION

# The tools are pinned so the bundle builds with the same Node every time. Their libraries float: this stage never ships.
RUN echo "Installing on $BUILDPLATFORM" \
    && apk add --no-cache \
        curl=8.22.0-r3 \
        bash=5.3-r13 \
        openssl=3.6.4-r8 \
        unzip=6.0-r8 \
        ca-certificates=20260909-r1 \
        "nodejs-${TOOLS_NODE_VERSION}=24.21.0-r3" \
        npm-12=12.1.0-r0 \
        node-gyp=13.0.2-r1 \
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
# nginx only — the UI is a static bundle now, so there is no node process and no
# node runtime in the final image.
FROM $BASE_IMAGE_REGISTRY/chainguard/wolfi-base:latest@sha256:08df5982c3d27e70a4ce1607e3bb9af09d746f8722cf135a7694afef879fc5a2 AS final
ENV LANG=C.UTF-8
ENV LC_ALL=C.UTF-8
# This is used to print the build platform in the logs
ARG BUILDPLATFORM

# Every package pinned, dependencies included, so a rebuild of the same commit ships the same image. List them with `apk info -v` after an unpinned install.
RUN echo "Installing on $BUILDPLATFORM" \
    && apk add --no-cache \
        bash=5.3-r13 \
        ca-certificates=20260909-r1 \
        curl=8.22.0-r3 \
        cyrus-sasl-heimdal-libs=2.1.28-r57 \
        gdbm=1.26-r6 \
        heimdal-libs=7.8.0-r52 \
        keyutils-libs=1.6.3-r40 \
        krb5-conf=1.0-r9 \
        krb5-libs=1.22.2-r4 \
        libcom_err=1.47.4-r2 \
        libcurl-openssl4=8.22.0-r3 \
        libidn2=2.3.8-r9 \
        libldap=2.6.10-r5 \
        libnghttp2-14=1.70.0-r4 \
        libpcre2-8-0=10.48-r0 \
        libpsl=0.23.3-r1 \
        libunistring=1.4.2-r3 \
        libverto=0.3.2-r8 \
        ncurses=6.6.20260919-r0 \
        ncurses-terminfo-base=6.6.20260919-r0 \
        nghttp3=1.18.0-r1 \
        nginx-mainline=1.31.6-r2 \
        nginx-mainline-config=1.31.6-r2 \
        nginx-mainline-fastcgi-params=1.31.6-r2 \
        nginx-mainline-mime-types=1.31.6-r2 \
        nginx-mainline-scgi-params=1.31.6-r2 \
        nginx-mainline-uwsgi-params=1.31.6-r2 \
        ngtcp2=1.25.0-r3 \
        openssl=3.6.4-r8 \
        readline=8.3-r3 \
        sqlite-libs=3.53.4-r2 \
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
