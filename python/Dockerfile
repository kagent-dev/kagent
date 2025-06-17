### STAGE 1: base image
ARG BASE_IMAGE_REGISTRY=cgr.dev
FROM $BASE_IMAGE_REGISTRY/chainguard/wolfi-base:latest AS base-os

ENV LANG=C.UTF-8
ENV LC_ALL=C.UTF-8
RUN apk update && apk add  \
    curl openssl bash git ca-certificates uv nodejs-22 npm \
    && rm -rf /var/cache/apk/*

### STAGE 2: python
FROM base-os AS python-os
ARG TOOLS_PYTHON_VERSION=3.12

ENV PYTHONDONTWRITEBYTECODE=1
ENV PYTHONUNBUFFERED=1
ENV GIT_LFS_SKIP_SMUDGE=1

ENV UV_LINK_MODE=copy
ENV UV_CACHE_DIR=/app/python/.cache/uv
ENV UV_PROJECT_ENVIRONMENT="/app/python/.venv"

RUN addgroup -g 1001 pythongroup                       && \
    adduser  -u 1001 -G pythongroup -s /bin/bash -D python -h /app/python/  && \
    mkdir    -p /app/python/bin                        && \
    mkdir    -p /app/python/downloads                  && \
    mkdir    -p /app/python/.cache/uv                  && \
    mkdir    -p /app/python/.autogenstudio/configs     && \
    chown    -vR 1001:1001 /app

USER python
WORKDIR /app/python

### STAGE 3: tools
FROM base-os AS tools-builder
ARG TARGETARCH

ARG TOOLS_BUN_VERSION
ARG TOOLS_HELM_VERSION
ARG TOOLS_ISTIO_VERSION
ARG TOOLS_ARGO_CD_VERSION

WORKDIR /app/python/downloads

RUN curl -LO "https://dl.k8s.io/release/$(curl -L -s https://dl.k8s.io/release/stable.txt)/bin/linux/$TARGETARCH/kubectl" \
    && chmod +x kubectl

RUN curl -Lo /app/python/downloads/kubectl-argo-rollouts -L https://github.com/argoproj/argo-rollouts/releases/$TOOLS_ARGO_CD_VERSION/download/kubectl-argo-rollouts-linux-$TARGETARCH \
    && chmod +x /app/python/downloads/kubectl-argo-rollouts

RUN curl -L https://istio.io/downloadIstio | ISTIO_VERSION=$TOOLS_ISTIO_VERSION TARGET_ARCH=$TARGETARCH sh - \
    && mv istio-*/bin/istioctl /app/python/downloads/                                                           \
    && rm -rf istio-*

# Install Helm
RUN curl -Lo helm.tar.gz https://get.helm.sh/helm-v${TOOLS_HELM_VERSION}-linux-${TARGETARCH}.tar.gz  \
    && tar -xvf helm.tar.gz                                                                             \
    && mv linux-${TARGETARCH}/helm /app/python/downloads/helm                                           \
    && chmod +x /app/python/downloads/helm

### STAGE 4: final
FROM python-os AS final
ARG TOOLS_PYTHON_VERSION

WORKDIR /app/python

COPY --from=tools-builder --chown=python:pythongroup /app/python/downloads/kubectl               /app/python/bin/kubectl
COPY --from=tools-builder --chown=python:pythongroup /app/python/downloads/istioctl              /app/python/bin/istioctl
COPY --from=tools-builder --chown=python:pythongroup /app/python/downloads/helm                  /app/python/bin/helm
COPY --from=tools-builder --chown=python:pythongroup /app/python/downloads/kubectl-argo-rollouts /app/python/bin/kubectl-argo-rollouts

COPY --chown=python:pythongroup pyproject.toml  .
COPY --chown=python:pythongroup .python-version .
COPY --chown=python:pythongroup uv.lock .
COPY --chown=python:pythongroup src src
COPY --chown=python:pythongroup README.md .

# Install dependencies
RUN echo "Installing dependencies..."                   \
    && uv venv --python=python$TOOLS_PYTHON_VERSION     \
    && uv sync --locked --refresh                       \
    && uv pip install hatchling hatch setuptools        \
    && find . | grep playwright | grep node | tee | xargs rm -rf      # remove cve playwtite node from venv

ENV UV_OFFLINE=1

# Generate configs
RUN mkdir -p /app/python/.autogenstudio/configs                  \
    && uv run tool_gen -o /app/python/.autogenstudio/configs

# Test if the tool is working and fetch all dependencies
RUN uv run kagent-engine --help

EXPOSE 8081
ARG VERSION

LABEL org.opencontainers.image.source=https://github.com/kagent-dev/kagent
LABEL org.opencontainers.image.description="Kagent app is the apiserver for running agents."
LABEL org.opencontainers.image.authors="Kagent Creators 🤖"
LABEL org.opencontainers.image.version="$VERSION"

CMD ["uv", "run", "kagent-engine", "serve", "--host", "0.0.0.0", "--port", "8081"]