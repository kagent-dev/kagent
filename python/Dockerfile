### STAGE 1: base image
ARG BASE_IMAGE_REGISTRY=cgr.dev
FROM $BASE_IMAGE_REGISTRY/chainguard/wolfi-base:latest AS base-os

ENV LANG=C.UTF-8
ENV LC_ALL=C.UTF-8
RUN apk update && apk add  \
    curl openssl unzip bash git ca-certificates

### STAGE 2: python
FROM base-os AS python-os
ARG TOOLS_PYTHON_VERSION=3.12

ENV PYTHONDONTWRITEBYTECODE=1
ENV PYTHONUNBUFFERED=1
RUN apk update && apk add python3 uv

ENV GIT_LFS_SKIP_SMUDGE=1
ENV UV_LINK_MODE=copy
ENV UV_PROJECT_ENVIRONMENT="/app/python/.venv"

WORKDIR /app/python
RUN addgroup -g 1001 pythongroup && \
    adduser -u 1001 -G pythongroup -s /bin/bash -D python && \
    chown -R python:pythongroup /app/python

RUN --mount=type=cache,target=/app/python/.cache,rw  \
    echo "Installing Python $TOOLS_PYTHON_VERSION..."                 \
    && uv venv .venv                                 \
    && uv python install $TOOLS_PYTHON_VERSION                        \
    && uv cache dir                                  \
    && uv version

### STAGE 3: tools
FROM python-os AS python-builder
ARG TARGETARCH

ARG TOOLS_BUN_VERSION
ENV BUN_INSTALL_CACHE_DIR=/cache/bun
ENV BUN_INSTALL=/usr/local/bun
ENV PATH=$BUN_INSTALL/bin:$PATH
ARG TOOLS_ISTIO_VERSION
ARG TOOLS_ARGO_CD_VERSION

RUN mkdir -p /app/downloads /usr/local/bin/
WORKDIR /app/downloads


# Install Bun (uses official install script)
# brew install oven-sh/bun/bun
RUN --mount=type=cache,target=/cache/bun,rw \
    mkdir -p $BUN_INSTALL  \
    && curl -fsSL https://bun.sh/install | bash -s "bun-v$TOOLS_BUN_VERSION" \
    && bun --version


RUN curl -LO "https://dl.k8s.io/release/$(curl -L -s https://dl.k8s.io/release/stable.txt)/bin/linux/$TARGETARCH/kubectl" \
    && chmod +x kubectl \
    && mv kubectl /usr/local/bin/

RUN curl -Lo /usr/local/bin/kubectl-argo-rollouts -L https://github.com/argoproj/argo-rollouts/releases/$TOOLS_ARGO_CD_VERSION/download/kubectl-argo-rollouts-linux-$TARGETARCH \
    && chmod +x /usr/local/bin/kubectl-argo-rollouts

RUN curl -L https://istio.io/downloadIstio | ISTIO_VERSION=$TOOLS_ISTIO_VERSION TARGET_ARCH=$TARGETARCH sh - \
    && mv istio-*/bin/istioctl /usr/local/bin/istioctl \
    && rm -rf istio-*

# Install Helm
RUN curl -fsSL -o get_helm.sh https://raw.githubusercontent.com/helm/helm/main/scripts/get-helm-3 \
    && chmod 700 get_helm.sh \
    && ./get_helm.sh \
    && rm get_helm.sh

### STAGE 4: final
FROM python-os AS final

ENV BUN_INSTALL_CACHE_DIR=/cache/bun
ENV BUN_INSTALL=/usr/local/bun
ENV PATH=$BUN_INSTALL/bin:$PATH

COPY --from=python-builder /usr/local/bin/kubectl /usr/local/bin/kubectl
COPY --from=python-builder /usr/local/bin/istioctl /usr/local/bin/istioctl
COPY --from=python-builder /usr/local/bin/helm /usr/local/bin/helm
COPY --from=python-builder /usr/local/bin/kubectl-argo-rollouts /usr/local/bin/kubectl-argo-rollouts
COPY --from=python-builder $BUN_INSTALL $BUN_INSTALL

COPY pyproject.toml .
COPY .python-version .
COPY uv.lock .
COPY README.md .
COPY src src

# Install dependencies
RUN --mount=type=cache,target=/app/python/.cache                  \
    --mount=type=bind,source=pyproject.toml,target=pyproject.toml \
    uv venv .venv && uv sync --locked

# Generate configs
RUN --mount=type=cache,target=/app/python/.cache                  \
    --mount=type=bind,source=pyproject.toml,target=pyproject.toml \
    mkdir -p /root/.autogenstudio/configs && uv run tool_gen -o /root/.autogenstudio/configs

# Test if the tool is working and fetch all dependencies
RUN --mount=type=cache,target=/app/python/.cache                  \
    --mount=type=bind,source=pyproject.toml,target=pyproject.toml \
    uv run kagent-engine --help

RUN chown -R python:pythongroup /app/python && \
    chmod -vR 755 /app/python/.venv/bin

ENV UV_OFFLINE=1
EXPOSE 8081

LABEL org.opencontainers.image.source=https://github.com/kagent-dev/kagent
LABEL org.opencontainers.image.description="Kagent app is the apiserver for running agents."
LABEL org.opencontainers.image.authors="Kagent Creators 🤖"
CMD ["uv", "run", "kagent-engine", "serve", "--host", "0.0.0.0", "--port", "8081"]