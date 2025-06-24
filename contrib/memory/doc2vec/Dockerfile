### STAGE 1: base image
ARG BASE_IMAGE_REGISTRY=cgr.dev
FROM --platform=linux/$BUILDARCH $BASE_IMAGE_REGISTRY/chainguard/wolfi-base:latest AS base-os

ENV LANG=C.UTF-8
ENV LC_ALL=C.UTF-8
RUN apk update \
    && apk add --no-cache python3 make build-base curl bash openssl unzip ca-certificates nodejs npm sqlite-dev node-gyp \
    && update-ca-certificates \
    && rm -rf /var/cache/apk/*

ARG TARGETARCH
ARG VERSION

WORKDIR /app

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

# Copy project files
COPY package.json /app/
COPY package-lock.json /app/
COPY tsconfig.json /app/
COPY src/index.ts /app/src/

RUN npm install typescript     \
    && npm install             \
    && npm run build

LABEL org.opencontainers.image.source=https://github.com/kagent-dev/kagent
LABEL org.opencontainers.image.description="Kagent app is the UI and apiserver for running agents."
LABEL org.opencontainers.image.authors="Kagent Creators 🤖"
LABEL org.opencontainers.image.version="$VERSION"

ENTRYPOINT ["node", "build/index.js"]