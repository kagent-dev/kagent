### STAGE 1: Dependencies and Build
ARG BASE_IMAGE_REGISTRY=cgr.dev
ARG BUILDPLATFORM
FROM --platform=$BUILDPLATFORM $BASE_IMAGE_REGISTRY/chainguard/wolfi-base:latest AS deps
ENV LANG=C.UTF-8
ENV LC_ALL=C.UTF-8
# This is used to print the build platform in the logs
ARG BUILDPLATFORM

RUN --mount=type=cache,target=/var/cache/apk,rw \
    echo "Installing on $BUILDPLATFORM" \
    && apk update \
    && apk add curl bash openssl unzip ca-certificates nginx supervisor \
    && update-ca-certificates

ARG TOOLS_BUN_VERSION
ARG TARGETARCH

ENV DO_NOT_TRACK=1
ENV NEXT_TELEMETRY_DISABLED=1
ENV BUN_INSTALL_CACHE_DIR=/cache/bun
ENV BUN_INSTALL=/usr/local/bun
ENV NODE_ENV=production
ENV CYPRESS_INSTALL_BINARY=0
ENV PATH=$BUN_INSTALL/bin:$PATH

# Install Bun (uses official install script)
# brew install oven-sh/bun/bun
RUN --mount=type=cache,target=/cache/bun,rw \
    mkdir -p $BUN_INSTALL  \
    && curl -fsSL https://bun.sh/install | bash -s "bun-v$TOOLS_BUN_VERSION" \
    && bun --version

WORKDIR /app/ui

# Copy package files and install dependencies
COPY package*.json ./
RUN --mount=type=cache,target=/cache/node_modules,rw \
    bun install --frozen-lockfile \
    && bun pm ls --all \
    && bun pm hash

### STAGE 2: Build
FROM --platform=$BUILDPLATFORM deps AS builder

# Copy source files
COPY . .

# Build the application (native compilation for speed)
RUN --mount=type=cache,target=/cache/node_modules,rw \
    --mount=type=cache,target=/app/ui/.next/cache,rw \
    export NEXT_TELEMETRY_DEBUG=1 \
    && bun install --frozen-lockfile \
    && bun run build --no-lint --turbopack \
    && mkdir -p /app/ui/public

### STAGE 3: Runtime
FROM $BASE_IMAGE_REGISTRY/chainguard/wolfi-base:latest AS final
ENV LANG=C.UTF-8
ENV LC_ALL=C.UTF-8
# This is used to print the build platform in the logs
ARG BUILDPLATFORM

RUN --mount=type=cache,target=/var/cache/apk,rw \
    echo "Installing on $BUILDPLATFORM" \
    && apk update \
    && apk add curl bash openssl unzip ca-certificates nginx supervisor \
    && update-ca-certificates

ARG TOOLS_BUN_VERSION
ENV BUN_INSTALL=/usr/local/bun
ENV PATH=$BUN_INSTALL/bin:$PATH
# Install Bun in native arch for running (uses official install script)
# brew install oven-sh/bun/bun
RUN mkdir -p $BUN_INSTALL  \
    && curl -fsSL https://bun.sh/install | bash -s "bun-v$TOOLS_BUN_VERSION" \
    && bun --version

RUN mkdir -p /app/ui/public /run/nginx/ /var/lib/nginx/tmp/ /var/lib/nginx/logs/  \
    && addgroup -g 1001    nginx                        \
    && adduser  -u 1001 -G nginx -s /bin/bash -D nextjs \
    && adduser  -u 1002 -G nginx -s /bin/bash -D nginx  \
    && chown    -vR nextjs:nginx /app/ui                \
    && chown    -vR nextjs:nginx /run/nginx             \
    && chown    -vR nextjs:nginx /var/lib/nginx/

WORKDIR /app
COPY conf/nginx.conf /etc/nginx/nginx.conf
COPY conf/supervisord.conf /etc/supervisor/conf.d/supervisord.conf

WORKDIR /app/ui
COPY --from=builder /app/ui/next.config.ts ./
COPY --from=builder /app/ui/public ./public
COPY --from=builder /app/ui/package.json ./package.json
COPY --from=builder --chown=nextjs:nginx /app/ui/.next/standalone ./
COPY --from=builder --chown=nextjs:nginx /app/ui/.next/static ./.next/static

# Ensure correct permissions
RUN chown -R nextjs:nginx /app/ui && \
    chmod -R 755 /app

EXPOSE 80
ARG VERSION

LABEL org.opencontainers.image.source=https://github.com/kagent-dev/kagent
LABEL org.opencontainers.image.description="Kagent app is the UI and apiserver for running agents."
LABEL org.opencontainers.image.authors="Kagent Creators 🤖"
LABEL org.opencontainers.image.version="$VERSION"

USER nextjs

CMD ["/usr/bin/supervisord", "-c", "/etc/supervisor/conf.d/supervisord.conf"]