### STAGE 1: base image
ARG DOCKER_REGISTRY=docker.io
ARG TOOLS_PYTHON_VERSION=3.12
FROM $DOCKER_REGISTRY/chainguard/wolfi-base:latest AS base-os

ENV LANG=C.UTF-8
ENV LC_ALL=C.UTF-8

RUN apk update && apk add  \
    curl openssl unzip bash git ca-certificates

### STAGE 2: python
FROM base-os AS node-os
ENV NEXT_TELEMETRY_DISABLED=1
ENV BUN_INSTALL_CACHE_DIR=/cache/bun
ENV BUN_INSTALL=/usr/local/bin/bun
ENV NVM_DIR=/usr/local/nvm
ENV NODE_ENV=production
ENV NODE_PATH=/opt/node_modules
ENV PATH=$BUN_INSTALL/bin:$PATH

# Install Bun (uses official install script)
# brew install oven-sh/bun/bun
RUN curl -fsSL https://bun.sh/install | bash \
    && bun --version

### STAGE 2: build + ui
FROM node-os AS ui-builder

WORKDIR /app/ui
COPY package*.json ./
RUN --mount=type=cache,target=/cache/bun,rw \
    bun install -- production --prefer-offline

COPY . .
RUN --mount=type=cache,target=/cache/bun,rw \
    bun run build                           \
    && mkdir -p /app/ui/public

### STAGE 3: final
FROM node-os AS final

RUN apk update  \
    && apk add nginx supervisor --no-cache              \
    && mkdir -p /app/ui                                 \
    && addgroup -g 1001 nodejs                          \
    && adduser -u 1001 -G nodejs -s /bin/bash -D nextjs \
    && chown -R nextjs:nodejs /app/ui

WORKDIR /app/ui

COPY --from=ui-builder /app/ui/next.config.ts ./
COPY --from=ui-builder /app/ui/public ./public
COPY --from=ui-builder /app/ui/package.json ./package.json
COPY --from=ui-builder --chown=nextjs:nodejs /app/ui/.next/standalone ./
COPY --from=ui-builder --chown=nextjs:nodejs /app/ui/.next/static ./.next/static

# Set up Nginx and Supervisor
WORKDIR /app
COPY conf/nginx.conf /etc/nginx/nginx.conf
COPY conf/supervisord.conf /etc/supervisor/conf.d/supervisord.conf

# Ensure correct permissions
RUN chown -R nextjs:nodejs /app/ui && \
    chmod -R 755 /app

EXPOSE 80

LABEL org.opencontainers.image.source=https://github.com/kagent-dev/kagent
LABEL org.opencontainers.image.description="Kagent app is the UI and apiserver for running agents."
LABEL org.opencontainers.image.authors="Kagent Creators 🤖"

CMD ["/usr/bin/supervisord", "-c", "/etc/supervisor/conf.d/supervisord.conf"]