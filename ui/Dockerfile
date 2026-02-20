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
    && apk add curl bash openssl unzip ca-certificates nginx supervisor nodejs-22 npm \
    && update-ca-certificates \
    && node --version \
    && npm --version

ARG TARGETARCH

ENV DO_NOT_TRACK=1
ENV NEXT_TELEMETRY_DISABLED=1
ENV CYPRESS_INSTALL_BINARY=0

WORKDIR /app/ui

# Copy package files for layer caching
COPY package*.json ./

### STAGE 2: Build
FROM --platform=$BUILDPLATFORM deps AS builder

# Copy source files
COPY . .

# Install all dependencies (including devDependencies for next build) and build
RUN --mount=type=cache,target=/root/.npm,rw \
    --mount=type=cache,target=/app/ui/.next/cache,rw \
    npm ci \
    && npm run build \
    && mkdir -p /app/ui/public

### STAGE 3: Runtime
FROM $BASE_IMAGE_REGISTRY/chainguard/wolfi-base:latest AS final
ENV LANG=C.UTF-8
ENV LC_ALL=C.UTF-8
ENV NODE_ENV=production
# This is used to print the build platform in the logs
ARG BUILDPLATFORM

RUN --mount=type=cache,id=apk-final,target=/var/cache/apk,rw \
    echo "Installing on $BUILDPLATFORM" \
    && apk update \
    && apk add curl bash openssl unzip ca-certificates nginx supervisor nodejs-22 \
    && update-ca-certificates \
    && node --version

RUN mkdir -p /app/ui/public /tmp/nginx/client_temp /tmp/nginx/proxy_temp /tmp/nginx/fastcgi_temp /tmp/nginx/uwsgi_temp /tmp/nginx/scgi_temp  \
    && addgroup -g 1001 nginx                        \
    && adduser  -u 1001 -G nginx -s /bin/bash -D nextjs \
    && adduser  -u 1002 -G nginx -s /bin/bash -D nginx  \
    && chown    -vR nextjs:nginx /app/ui                \
    && chown    -vR nextjs:nginx /tmp/nginx/

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

EXPOSE 8080
ARG VERSION

LABEL org.opencontainers.image.source=https://github.com/kagent-dev/kagent
LABEL org.opencontainers.image.description="Kagent app is the UI and apiserver for running agents."
LABEL org.opencontainers.image.authors="Kagent Creators 🤖"
LABEL org.opencontainers.image.version="$VERSION"

USER nextjs

CMD ["/usr/bin/supervisord", "-c", "/etc/supervisor/conf.d/supervisord.conf"]
