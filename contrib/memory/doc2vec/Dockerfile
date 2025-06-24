### STAGE 1: base image
ARG BASE_IMAGE_REGISTRY=cgr.dev
FROM --platform=linux/$BUILDARCH $BASE_IMAGE_REGISTRY/chainguard/wolfi-base:latest AS builder

ENV LANG=C.UTF-8
ENV LC_ALL=C.UTF-8

RUN apk update && apk add --no-cache \
    bash \
    build-base \
    ca-certificates \
    curl \
    make \
    node-gyp \
    nodejs \
    npm \
    openssl \
    python3 \
    sqlite-dev \
    unzip \
    && update-ca-certificates \
    && rm -rf /var/cache/apk/*

WORKDIR /app

# Copy package files first for better layer caching
COPY package.json package-lock.json tsconfig.json ./
COPY src/ ./src/

# Install dependencies and build
RUN --mount=type=cache,target=/root/.npm \
    npm install typescript && \
    npm ci --only=production && \
    npm install --save-dev typescript && \
    npm run build

# Production stage
ARG BASE_IMAGE_REGISTRY=cgr.dev
FROM --platform=linux/$BUILDARCH $BASE_IMAGE_REGISTRY/chainguard/wolfi-base:latest AS production

ENV LANG=C.UTF-8
ENV LC_ALL=C.UTF-8
ENV NODE_ENV=production

# Install only runtime dependencies
RUN apk update && apk add --no-cache \
    nodejs \
    ca-certificates \
    sqlite \
    && update-ca-certificates \
    && rm -rf /var/cache/apk/*

# Create non-root user
RUN addgroup -g 1001 -S nodejs && \
    adduser -S kagent -u 1001 -G nodejs

WORKDIR /app

# Copy built application and production dependencies from builder stage
COPY --from=builder --chown=kagent:nodejs /app/build ./build
COPY --from=builder --chown=kagent:nodejs /app/node_modules ./node_modules
COPY --from=builder --chown=kagent:nodejs /app/package.json ./

# copy data files

ADD https://doc-sqlite-db.s3.sa-east-1.amazonaws.com/kubernetes.db /app/build/kubernetes.db
ADD https://doc-sqlite-db.s3.sa-east-1.amazonaws.com/istio.db /app/build/istio.db
ADD https://doc-sqlite-db.s3.sa-east-1.amazonaws.com/argo.db /app/build/argo.db
ADD https://doc-sqlite-db.s3.sa-east-1.amazonaws.com/argo-rollouts.db /app/build/argo-rollouts.db
ADD https://doc-sqlite-db.s3.sa-east-1.amazonaws.com/helm.db /app/build/helm.db
ADD https://doc-sqlite-db.s3.sa-east-1.amazonaws.com/prometheus.db /app/build/prometheus.db
ADD https://doc-sqlite-db.s3.sa-east-1.amazonaws.com/gateway-api.db /app/build/gateway-api.db
ADD https://doc-sqlite-db.s3.sa-east-1.amazonaws.com/gloo-gateway.db /app/build/gloo-gateway.db
ADD https://doc-sqlite-db.s3.sa-east-1.amazonaws.com/kgateway.db /app/build/kgateway.db
ADD https://doc-sqlite-db.s3.sa-east-1.amazonaws.com/gloo-edge.db /app/build/gloo-edge.db
ADD https://doc-sqlite-db.s3.sa-east-1.amazonaws.com/otel.db /app/build/otel.db
ADD https://doc-sqlite-db.s3.sa-east-1.amazonaws.com/cilium.db /app/build/cilium.db
ADD https://doc-sqlite-db.s3.sa-east-1.amazonaws.com/ambient.db /app/build/ambient.db

USER kagent

LABEL org.opencontainers.image.source=https://github.com/kagent-dev//kagent-doc2vec
LABEL org.opencontainers.image.description="Kagent Doc2Vec MCP"
LABEL org.opencontainers.image.authors="Kagent Creators 🤖"

EXPOSE 8085

ENTRYPOINT ["node", "build/index.js"]