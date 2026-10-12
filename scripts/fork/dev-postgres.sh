#!/usr/bin/env bash
# A development PostgreSQL for `kagent install --skip-database-setup` on the
# Giant Swarm line (FORK.md, "Which kagent are we running"): the bundled one of
# `kagent install` authenticates with postgres.podcert.ate.dev certificates,
# which the line's Substrate does not sign. This one trusts every connection
# inside the cluster and is for kind clusters only: no TLS, no password.
#
#   scripts/fork/dev-postgres.sh [namespace]   # default kagent
#
# Writes the Secret kagent-postgres the kagent chart reads; install kagent with
# --set database.postgres.role= (the login user owns the database). Substrate
# keeps the database its own chart deploys.
set -euo pipefail

NAMESPACE=${1:-kagent}
IMAGE=${KAGENT_BUNDLED_POSTGRES_IMAGE:-postgres:18-alpine}

kubectl create namespace "$NAMESPACE" --dry-run=client -o yaml | kubectl apply -f -
kubectl apply -n "$NAMESPACE" -f - <<EOF
apiVersion: apps/v1
kind: Deployment
metadata:
  name: kagent-postgresql
  labels: {app.kubernetes.io/name: kagent-postgresql}
spec:
  replicas: 1
  strategy: {type: Recreate}
  selector:
    matchLabels: {app.kubernetes.io/name: kagent-postgresql}
  template:
    metadata:
      labels: {app.kubernetes.io/name: kagent-postgresql}
    spec:
      containers:
      - name: postgresql
        image: ${IMAGE}
        env:
        - {name: POSTGRES_DB, value: kagent}
        - {name: POSTGRES_USER, value: postgres}
        - {name: POSTGRES_HOST_AUTH_METHOD, value: trust}
        ports:
        - {name: postgresql, containerPort: 5432}
        readinessProbe:
          exec:
            command: [pg_isready, -U, postgres, -d, kagent]
          periodSeconds: 5
---
apiVersion: v1
kind: Service
metadata:
  name: kagent-postgresql
spec:
  selector: {app.kubernetes.io/name: kagent-postgresql}
  ports:
  - {name: postgresql, port: 5432, targetPort: postgresql}
---
apiVersion: v1
kind: Secret
metadata:
  name: kagent-postgres
type: Opaque
stringData:
  connectionString: postgresql://postgres@kagent-postgresql.${NAMESPACE}.svc:5432/kagent?sslmode=disable
EOF
kubectl -n "$NAMESPACE" rollout status deployment/kagent-postgresql --timeout=5m
