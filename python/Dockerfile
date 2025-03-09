FROM ghcr.io/astral-sh/uv:0.6.5-debian-slim AS builder 

ARG TARGETARCH

RUN apt-get update && apt-get install -y --no-install-recommends \
    git \
    build-essential \
    curl \
    ca-certificates \
    && apt-get update \
    && update-ca-certificates \
    && rm -rf /var/lib/apt/lists/*

# Install kubectl
RUN curl -LO "https://dl.k8s.io/release/$(curl -L -s https://dl.k8s.io/release/stable.txt)/bin/linux/$TARGETARCH/kubectl" \
    && chmod +x kubectl \
    && mv kubectl /usr/local/bin/

# Install Istio
RUN curl -L https://istio.io/downloadIstio | ISTIO_VERSION=1.25.0 TARGET_ARCH=$TARGETARCH sh - \
    && mv istio-1.25.0/bin/istioctl /usr/local/bin/istioctl \
    && rm -rf istio-1.25.0

# Install Helm
RUN curl -fsSL -o get_helm.sh https://raw.githubusercontent.com/helm/helm/main/scripts/get-helm-3 \
    && chmod 700 get_helm.sh \
    && ./get_helm.sh \
    && rm get_helm.sh

FROM ghcr.io/astral-sh/uv:0.6.5-debian-slim AS final

ARG TARGETARCH

RUN apt-get update && apt-get install -y --no-install-recommends \
    git \
    ca-certificates \
    && update-ca-certificates \
    && rm -rf /var/lib/apt/lists/*

COPY --from=builder /usr/local/bin/kubectl /usr/local/bin/kubectl
COPY --from=builder /usr/local/bin/istioctl /usr/local/bin/istioctl
COPY --from=builder /usr/local/bin/helm /usr/local/bin/helm

ENV PYTHONUNBUFFERED=1 \
    PYTHONDONTWRITEBYTECODE=1 \
    PIP_NO_CACHE_DIR=1 \
    PATH="/usr/local/bin:/usr/local/sbin:/usr/bin:/usr/sbin:/sbin:/bin:/root/.cargo/bin"

RUN uv python install 3.12

# Create groups and users
RUN groupadd -g 1002 pythongroup \
    && useradd -u 1002 -g pythongroup -s /bin/bash -m python \
    && mkdir -p /app/python \
    && chown -R python:pythongroup /app/python

# Set up Python backend
WORKDIR /app/python
COPY pyproject.toml .
COPY .python-version .
COPY uv.lock .
COPY src src
COPY README.md .
RUN uv sync --frozen && \
    chown -R python:pythongroup /app/python

# Generate tools and agents
RUN mkdir -p /root/.autogenstudio/configs
RUN uv run tool_gen -o /root/.autogenstudio/configs
COPY agents/*.json /root/.autogenstudio/configs

# Ensure correct permissions
RUN chown -R python:pythongroup /app/python && \
    chmod -R 755 /app

RUN mkdir -p /app/python/.cache/uv && \
    chown -R python:pythongroup /app/python/.cache

EXPOSE 8081

LABEL org.opencontainers.image.source=https://github.com/kagent-dev/kagent
LABEL org.opencontainers.image.description="Kagent app is the apiserver for running agents."

CMD ["uv", "run", "autogenstudio", "ui", "--host", "0.0.0.0", "--port", "8081"]
