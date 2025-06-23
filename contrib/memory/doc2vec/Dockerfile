FROM node:20-slim AS base

WORKDIR /app

ADD https://doc-sqlite-db.s3.sa-east-1.amazonaws.com/kubernetes.db /app/kubernetes.db
ADD https://doc-sqlite-db.s3.sa-east-1.amazonaws.com/istio.db /app/istio.db
ADD https://doc-sqlite-db.s3.sa-east-1.amazonaws.com/argo.db /app/argo.db
ADD https://doc-sqlite-db.s3.sa-east-1.amazonaws.com/argo-rollouts.db /app/argo-rollouts.db
ADD https://doc-sqlite-db.s3.sa-east-1.amazonaws.com/helm.db /app/helm.db
ADD https://doc-sqlite-db.s3.sa-east-1.amazonaws.com/prometheus.db /app/prometheus.db
ADD https://doc-sqlite-db.s3.sa-east-1.amazonaws.com/gateway-api.db /app/gateway-api.db
ADD https://doc-sqlite-db.s3.sa-east-1.amazonaws.com/gloo-gateway.db /app/gloo-gateway.db
ADD https://doc-sqlite-db.s3.sa-east-1.amazonaws.com/kgateway.db /app/kgateway.db
ADD https://doc-sqlite-db.s3.sa-east-1.amazonaws.com/gloo-edge.db /app/gloo-edge.db
ADD https://doc-sqlite-db.s3.sa-east-1.amazonaws.com/otel.db /app/otel.db
ADD https://doc-sqlite-db.s3.sa-east-1.amazonaws.com/cilium.db /app/cilium.db
ADD https://doc-sqlite-db.s3.sa-east-1.amazonaws.com/ambient.db /app/ambient.db

# Copy project files
COPY package.json /app/
COPY package-lock.json /app/
COPY tsconfig.json /app/
COPY src/index.ts /app/src/

RUN npm install typescript  \
    && npm install          \
    && npm run build

ENTRYPOINT ["node", "build/index.js"]