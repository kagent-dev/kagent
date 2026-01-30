# Temporary: use apk to install krane
# once https://github.com/wolfi-dev/os/pull/78579 is merged.
### STAGE 0: build krane
ARG BASE_IMAGE_REGISTRY=cgr.dev
FROM $BASE_IMAGE_REGISTRY/chainguard/go:latest AS krane-builder

ENV KRANE_VERSION=v0.20.7
WORKDIR /build

RUN git clone --depth 1 --branch $KRANE_VERSION \
    https://github.com/google/go-containerregistry.git

WORKDIR /build/go-containerregistry/cmd/krane

RUN --mount=type=cache,target=/root/.cache/go-build \
    --mount=type=cache,target=/go/pkg/mod \
    CGO_ENABLED=0 go build \
    -trimpath \
    -ldflags="-s -w" \
    -o /build/krane .
    
### STAGE 1: base image
ARG BASE_IMAGE_REGISTRY=cgr.dev
FROM $BASE_IMAGE_REGISTRY/chainguard/wolfi-base:latest AS base-os

# Build arg to control SSL verification (set DISABLE_SSL_VERIFY=1 to skip SSL checks)
ARG DISABLE_SSL_VERIFY=0

ENV LANG=C.UTF-8
ENV LC_ALL=C.UTF-8

# Install packages with conditional SSL verification
# When DISABLE_SSL_VERIFY=1, use --no-check-certificate to bypass SSL checks (development only)
RUN --mount=type=cache,target=/var/cache/apk,rw \
    if [ "$DISABLE_SSL_VERIFY" = "1" ]; then \
        echo "WARNING: Disabling SSL verification for apk (development only)"; \
        apk update --no-check-certificate && apk add --no-check-certificate \
            curl openssl bash git ca-certificates uv libstdc++; \
    else \
        apk update && apk add \
            curl openssl bash git ca-certificates uv libstdc++; \
    fi

### STAGE 2: python
FROM base-os AS python-os
ARG TOOLS_PYTHON_VERSION=3.13

ENV PYTHONOPTIMIZE=2
ENV PYTHONUNBUFFERED=1

# Optimize malloc for containerized Python workloads
# 256KB threshold balances memory efficiency with performance
ENV MALLOC_TRIM_THRESHOLD_=262144
ENV MALLOC_ARENA_MAX=2

ENV GIT_LFS_SKIP_SMUDGE=1

ENV UV_LINK_MODE=copy
ENV UV_COMPILE_BYTECODE=1
ENV UV_COMPILE_BYTECODE_TIMEOUT=300
ENV UV_SYSTEM_PYTHON=1
ENV UV_NO_PROGRESS=1
ENV UV_HTTP_TIMEOUT=60
ENV UV_CONCURRENT_DOWNLOADS=10

# Configure the Python directories
ENV UV_CACHE_DIR=/.kagent/cache/packages
ENV UV_TOOL_DIR=/.kagent/cache/tools
ENV UV_PYTHON_DOWNLOADS_DIR=/.kagent/cache/downloads
ENV UV_PROJECT_ENVIRONMENT=/.kagent/.venv

ENV UV_PYTHON_INSTALL_DIR=/python
ENV UV_PYTHON_PREFERENCE=only-managed

RUN addgroup -g 1001 pythongroup                           && \
    adduser  -u 1001 -G pythongroup -s /bin/bash -D python -h /.kagent/  && \
    mkdir    -p $UV_PYTHON_DOWNLOADS_DIR                   && \
    mkdir    -p $UV_TOOL_DIR                               && \
    mkdir    -p $UV_CACHE_DIR                              && \
    mkdir    -p /python                                    && \
    chown    -vR 1001:1001 /.kagent /python

# Install anthropic sandbox runtime and dependencies
RUN --mount=type=cache,target=/var/cache/apk,rw \
    apk add \
    nodejs npm bubblewrap socat ripgrep

# Install sandbox runtime from a specific commit of the GitHub repo without using global prefix
# This avoids scope-related rename issues in global node_modules
# Using BuildKit cache for npm to speed up rebuilds
RUN --mount=type=cache,target=/root/.npm \
    mkdir -p /opt && \
    cd opt && \
    git clone --depth 1 --revision=771f18664f97f6606c9ed64fc65f988b3ecf2e69 https://github.com/anthropic-experimental/sandbox-runtime.git && \
    cd sandbox-runtime && \
    npm install && \
    npm run build && \
    npm install -g

# Ensure the sandbox runtime binaries are on PATH
ENV PATH="/opt/sandbox-runtime/node_modules/.bin:$PATH"

# Install anthropic sandbox runtime and dependencies
#RUN --mount=type=cache,target=/var/cache/apk,rw \
#    apk add krane
COPY --from=krane-builder --chown=1001:1001 /build/krane /usr/local/bin/krane

USER python
WORKDIR /.kagent

### STAGE 3: final
FROM python-os AS builder
ARG TOOLS_PYTHON_VERSION

WORKDIR /.kagent

ENV PATH=$PATH:/.kagent/bin:/.kagent/.venv/bin

# Copy dependency files first for better layer caching
COPY --chown=python:pythongroup pyproject.toml .
COPY --chown=python:pythongroup .python-version .
COPY --chown=python:pythongroup uv.lock .
COPY --chown=python:pythongroup packages/kagent-adk packages/kagent-adk
COPY --chown=python:pythongroup packages/kagent-core packages/kagent-core
COPY --chown=python:pythongroup packages/kagent-skills packages/kagent-skills
COPY --chown=python:pythongroup README.md .

ARG VERSION

# Install dependencies - make sure /.kagent/.venv/bin in path and not in cache mount
RUN --mount=type=cache,target=/.kagent/cache,uid=1001,gid=1001                \
    echo "Creating virtual environment and installing dependencies..."        \
    && uv venv --python=python$TOOLS_PYTHON_VERSION                           \
    && uv lock && uv sync --package kagent-adk                                \
    && uv cache prune                                                         \
    && echo "Installation complete."

# Create a separate venv for bash tool commands (sandbox environment)
# This venv does not have pip installed
RUN --mount=type=cache,target=/.kagent/cache,uid=1001,gid=1001                \
    echo "Creating bash tool sandbox environment..."                          \
    && mkdir -p /.kagent/sandbox-venv                                         \
    && uv venv --python=python$TOOLS_PYTHON_VERSION /.kagent/sandbox-venv     \
    && echo "Bash tool sandbox environment created."

ENV PATH="/.kagent/.venv/bin:$PATH"
ENV UV_PROJECT_ENVIRONMENT=/app/.venv
ENV BASH_VENV_PATH=/.kagent/sandbox-venv
ENV VIRTUAL_ENV=/.kagent/.venv

WORKDIR /app

ENTRYPOINT ["kagent-adk", "run", "--host", "0.0.0.0", "--port", "8080"]