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

RUN apt-get update && apt-get install -y --no-install-recommends \
    nginx \
    supervisor \
    && apt-get clean \
    && rm -rf /var/lib/apt/lists/*

### STAGE 2: build + tools
FROM --platform=$BUILDPLATFORM base AS builder

WORKDIR /app/

ARG TARGETARCH
ARG TOOLS_ISTIO_VERSION
ARG TOOLS_ARGO_CD_VERSION

# Install kubectl
RUN curl -LO "https://dl.k8s.io/release/$(curl -L -s https://dl.k8s.io/release/stable.txt)/bin/linux/$TARGETARCH/kubectl" \
    && chmod +x kubectl \
    && mv kubectl /usr/local/bin/

# Install kubectl argo plugin
RUN curl -o /usr/local/bin/kubectl-argo-rollouts -L https://github.com/argoproj/argo-rollouts/releases/$TOOLS_ARGO_CD_VERSION/download/kubectl-argo-rollouts-linux-$TARGETARCH \
    && chmod +x /usr/local/bin/kubectl-argo-rollouts

# Install Istio
RUN curl -L https://istio.io/downloadIstio | TOOLS_ISTIO_VERSION=$TOOLS_ISTIO_VERSION TARGET_ARCH=$TARGETARCH sh - \
    && mv istio-$TOOLS_ISTIO_VERSION/bin/istioctl /usr/local/bin/istioctl \
    && rm -rf istio-$TOOLS_ISTIO_VERSION

# Install Helm
RUN curl -fsSL -o get_helm.sh https://raw.githubusercontent.com/helm/helm/main/scripts/get-helm-3 \
    && chmod 700 get_helm.sh \
    && ./get_helm.sh \
    && rm get_helm.sh

### STAGE 3: final
FROM --platform=$BUILDPLATFORM base AS final
ARG TOOLS_NODE_VERSION
ARG TARGETARCH

COPY --from=builder /usr/local/bin/kubectl /usr/local/bin/kubectl
COPY --from=builder /usr/local/bin/istioctl /usr/local/bin/istioctl
COPY --from=builder /usr/local/bin/helm /usr/local/bin/helm

ENV PYTHONUNBUFFERED=1 \
    PYTHONDONTWRITEBYTECODE=1 \
    PIP_NO_CACHE_DIR=1 \
    UV_LINK_MODE=copy  \
    PATH="/usr/local/bin:/usr/local/sbin:/usr/bin:/usr/sbin:/sbin:/bin:/root/.cargo/bin"

# Create groups and users
RUN groupadd -g 1002 pythongroup \
    && useradd  -u 1002 -g pythongroup -s /bin/bash -m python \
    && mkdir -p /app/python /root/.cache/uv \
    && chown -R python:pythongroup /app/python

# Set up Python backend
WORKDIR /app/python
COPY pyproject.toml .
COPY .python-version .
COPY uv.lock .
COPY README.md .
RUN --mount=type=cache,target=/root/.cache/uv uv cache dir && uv venv .venv && uv pip install hatchling
RUN --mount=type=cache,target=/root/.cache/uv uv sync --frozen
COPY src src
RUN chown -R python:pythongroup /app/python

# Generate tools and agents
RUN mkdir -p /root/.autogenstudio/configs
RUN --mount=type=cache,target=/root/.cache/uv uv run tool_gen -o /root/.autogenstudio/configs

# Ensure correct permissions
RUN chown -R python:pythongroup /app/python && \
    chmod -R 755 /app

RUN mkdir -p /app/python/.cache/uv && \
    chown -R python:pythongroup /app/python/.cache

EXPOSE 8081

LABEL org.opencontainers.image.source=https://github.com/kagent-dev/kagent
LABEL org.opencontainers.image.description="Kagent app is the apiserver for running agents."

CMD ["uv", "run", "kagent-engine", "serve", "--host", "0.0.0.0", "--port", "8081"]
