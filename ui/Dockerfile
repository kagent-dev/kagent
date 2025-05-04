ARG TOOLS_UV_VERSION=latest
ARG TOOLS_NODE_VERSION=20
FROM --platform=$BUILDPLATFORM node:${TOOLS_NODE_VERSION}-bookworm AS ui-builder

WORKDIR /app/ui

COPY package*.json ./
RUN mkdir -p /app/ui/public
ENV NEXT_TELEMETRY_DISABLED=1

RUN npm ci

COPY . .
RUN npm run build

FROM ghcr.io/astral-sh/uv:${TOOLS_UV_VERSION}-debian-slim AS final

RUN apt-get update && apt-get install -y --no-install-recommends \
    git \
    build-essential \
    curl \
    ca-certificates \
    nginx \
    supervisor \
    gnupg \
    && curl -fsSL https://deb.nodesource.com/gpgkey/nodesource-repo.gpg.key | gpg --dearmor -o /etc/apt/keyrings/nodesource.gpg \
    && echo "deb [signed-by=/etc/apt/keyrings/nodesource.gpg] https://deb.nodesource.com/node_18.x nodistro main" | tee /etc/apt/sources.list.d/nodesource.list \
    && apt-get update \
    && apt-get install -y nodejs \
    && update-ca-certificates \
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
