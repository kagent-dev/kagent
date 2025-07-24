### STAGE 1: base image
ARG BASE_IMAGE_REGISTRY=cgr.dev
FROM $BASE_IMAGE_REGISTRY/chainguard/wolfi-base:latest AS base-os

ENV LANG=C.UTF-8
ENV LC_ALL=C.UTF-8
RUN --mount=type=cache,target=/var/cache/apk,rw \
    apk update && apk add  \
    curl openssl bash git ca-certificates uv nodejs-22 npm

### STAGE 2: python
FROM base-os AS python-os
ARG TOOLS_PYTHON_VERSION=3.12

ENV PYTHONDONTWRITEBYTECODE=1
ENV PYTHONUNBUFFERED=1
ENV GIT_LFS_SKIP_SMUDGE=1

ENV UV_LINK_MODE=symlink
ENV UV_CACHE_DIR=/app/python/.cache/uv
ENV UV_PROJECT_ENVIRONMENT="/app/python/.venv"

RUN addgroup -g 1001 pythongroup                       && \
    adduser  -u 1001 -G pythongroup -s /bin/bash -D python -h /app/python/  && \
    mkdir    -p /app/python/bin                        && \
    mkdir    -p /app/python/downloads                  && \
    mkdir    -p /app/python/.cache/uv                  && \
    chown    -vR 1001:1001 /app

USER python
WORKDIR /app/python

### STAGE 3 : download tools
FROM mcp/grafana:latest AS mcp-grafana
ARG TOOLS_GRAFANA_MCP_VERSION
LABEL org.opencontainers.image.source="mcp/grafana${TOOLS_GRAFANA_MCP_VERSION}"

### STAGE 4: final
FROM python-os AS final
ARG TOOLS_PYTHON_VERSION

WORKDIR /app/python

ENV PATH=$PATH:/app/python/bin

COPY --chown=python:pythongroup pyproject.toml  .
COPY --chown=python:pythongroup .python-version .
COPY --chown=python:pythongroup uv.lock .
COPY --chown=python:pythongroup src src
COPY --chown=python:pythongroup README.md .

# Install dependencies
RUN echo "Installing dependencies..."                   \
    && uv venv --python=python$TOOLS_PYTHON_VERSION     \
    && uv sync --locked --refresh                       \
    && uv pip install setuptools wheel                  \
    && find .venv -name "*playwright*" -name "*node*" -type d -exec rm -rf {} + 2>/dev/null || true \
    && uv run kagent-engine --help \
    && uv cache prune

# Offline mode
ENV UV_OFFLINE=1

# Test if the tool is working and fetch all dependencies
RUN uv run kagent-engine --help

 # Grafana MCP
COPY --from=mcp-grafana /app/mcp-grafana /app/python/bin/mcp-grafana

EXPOSE 8081
ARG VERSION

LABEL org.opencontainers.image.source=https://github.com/kagent-dev/kagent
LABEL org.opencontainers.image.description="Kagent app is the apiserver for running agents."
LABEL org.opencontainers.image.authors="Kagent Creators 🤖"
LABEL org.opencontainers.image.version="$VERSION"

CMD ["uv", "run", "kagent-engine", "--host", "0.0.0.0", "--port", "8081"]
