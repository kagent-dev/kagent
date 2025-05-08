ARG DOCKER_REGISTRY=ghcr.io
ARG TOOLS_UV_VERSION=latest
ARG PROXY
ARG NOPROXY

### STAGE 1: base image
FROM --platform=$BUILDPLATFORM $DOCKER_REGISTRY/astral-sh/uv:${TOOLS_UV_VERSION}-bookworm-slim AS base-os
ARG TOOLS_NODE_VERSION

#ENVIRONMENT VARIABLES
ENV DEBIAN_FRONTEND=noninteractive

RUN mkdir -p /etc/apt/keyrings \
    && apt-get update && apt-get install -y --no-install-recommends \
    git                       \
    ca-certificates           \
    curl                      \
    gnupg                     \
    wget                      \
    && update-ca-certificates \
    && rm -rf /var/lib/apt/lists/* \
    && ln -sf /usr/bin/python3 /usr/bin/python

FROM --platform=$BUILDPLATFORM base-os AS base

# Install python
RUN uv python install 3.12

# Install nvm
ENV NVM_DIR=/usr/local/nvm
ENV NEXT_TELEMETRY_DISABLED=1
ENV PATH=$NVM_DIR/versions/node/v$TOOLS_NODE_VERSION/bin:$PATH
ENV NODE_PATH=$NVM_DIR/v$TOOLS_NODE_VERSION/lib/node_modules

# Install nvm with node and npm
RUN mkdir -p  $NVM_DIR \
    && curl https://raw.githubusercontent.com/nvm-sh/nvm/v0.40.3/install.sh  | bash \
    && . $NVM_DIR/nvm.sh \
    && mkdir -p $NVM_DIR/versions \
    && nvm install $TOOLS_NODE_VERSION \
    && nvm use $TOOLS_NODE_VERSION \
    && which node && node --version

### STAGE 2: build + tools
FROM --platform=$BUILDPLATFORM base AS ui-builder

WORKDIR /app/ui

COPY package*.json ./
RUN mkdir -p /app/ui/public

RUN npm ci

COPY . .
RUN npm run build

### STAGE 3: final
FROM base AS final

RUN apt-get update && apt-get install -y --no-install-recommends \
    nginx \
    supervisor \
    && rm -rf /var/lib/apt/lists/* \
    && ln -sf /usr/bin/python3 /usr/bin/python

# Create groups and users
RUN groupadd -g 1001 nodejs \
    && groupadd -g 1002 pythongroup \
    && useradd -u 1001 -g nodejs -s /bin/bash -m nextjs \
    && useradd -u 1002 -g pythongroup -s /bin/bash -m python \
    && mkdir -p /app/python /app/ui /run/nginx \
    && chown -R python:pythongroup /app/python \
    && chown -R nextjs:nodejs /app/ui

# Set up Next.js UI
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

CMD ["/usr/bin/supervisord", "-c", "/etc/supervisor/conf.d/supervisord.conf"]
