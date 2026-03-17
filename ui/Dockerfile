### STAGE 1: Dependencies and Build
ARG BASE_IMAGE_REGISTRY=cgr.dev
ARG TOOLS_NODE_VERSION=24.13.0
ARG BUILDPLATFORM
FROM --platform=$BUILDPLATFORM $BASE_IMAGE_REGISTRY/chainguard/wolfi-base:latest AS deps
ENV LANG=C.UTF-8
ENV LC_ALL=C.UTF-8
# This is used to print the build platform in the logs
ARG BUILDPLATFORM
ARG TOOLS_NODE_VERSION

RUN echo "Installing on $BUILDPLATFORM" \
    && apk add --no-cache curl bash openssl unzip ca-certificates nginx supervisor "nodejs~${TOOLS_NODE_VERSION}" npm \
    && update-ca-certificates

ENV DO_NOT_TRACK=1
ENV NEXT_TELEMETRY_DISABLED=1
ENV CYPRESS_INSTALL_BINARY=0

WORKDIR /app/ui

# Copy package files and install dependencies
COPY package*.json ./
RUN --mount=type=cache,target=/root/.npm,rw \
    npm ci

### STAGE 2: Build
FROM --platform=$BUILDPLATFORM deps AS builder

# Copy source files
COPY . .

# Build the application (native compilation for speed)
RUN --mount=type=cache,target=/root/.npm,rw \
    --mount=type=cache,target=/app/ui/.next/cache,rw \
    export NEXT_TELEMETRY_DEBUG=1 \
    && npm run build \
    && mkdir -p /app/ui/public

### STAGE 3: Runtime
FROM $BASE_IMAGE_REGISTRY/chainguard/wolfi-base:latest AS final
ENV LANG=C.UTF-8
ENV LC_ALL=C.UTF-8
ENV NODE_ENV=production
# This is used to print the build platform in the logs
ARG BUILDPLATFORM
ARG TOOLS_NODE_VERSION

RUN echo "Installing on $BUILDPLATFORM" \
    && apk add --no-cache curl bash openssl unzip ca-certificates nginx supervisor "nodejs~${TOOLS_NODE_VERSION}" \
    && update-ca-certificates

RUN mkdir -p /app/ui/public /tmp/nginx/client_temp /tmp/nginx/proxy_temp /tmp/nginx/fastcgi_temp /tmp/nginx/uwsgi_temp /tmp/nginx/scgi_temp  \
    && addgroup -g 1001 nginx                        \
    && adduser  -u 1001 -G nginx -s /bin/bash -D nextjs \
    && adduser  -u 1002 -G nginx -s /bin/bash -D nginx  \
    && chown    -vR nextjs:nginx /app/ui                \
    && chown    -vR nextjs:nginx /tmp/nginx/

WORKDIR /app
COPY conf/nginx.conf /etc/nginx/nginx.conf
COPY conf/supervisord.conf /etc/supervisor/conf.d/supervisord.conf
COPY scripts/init.sh /usr/local/bin/init.sh

WORKDIR /app/ui
COPY --from=builder /app/ui/next.config.ts ./
COPY --from=builder /app/ui/public ./public
COPY --from=builder /app/ui/package.json ./package.json
COPY --from=builder --chown=nextjs:nginx /app/ui/.next/standalone ./
COPY --from=builder --chown=nextjs:nginx /app/ui/.next/static ./.next/static

# Ensure correct permissions
RUN chown -R nextjs:nginx /app/ui && \
    chmod -R 755 /app && \
    chmod +x /usr/local/bin/init.sh

EXPOSE 8080
ARG VERSION

LABEL org.opencontainers.image.source=https://github.com/kagent-dev/kagent
LABEL org.opencontainers.image.description="Kagent app is the UI and apiserver for running agents."
LABEL org.opencontainers.image.authors="Kagent Creators 🤖"
LABEL org.opencontainers.image.version="$VERSION"

USER 1001

CMD ["/usr/local/bin/init.sh"]
