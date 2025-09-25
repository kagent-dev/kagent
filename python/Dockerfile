### STAGE 1: base image
ARG BASE_IMAGE_REGISTRY=cgr.dev
FROM $BASE_IMAGE_REGISTRY/chainguard/wolfi-base:latest AS base-os

ENV LANG=C.UTF-8
ENV LC_ALL=C.UTF-8
RUN --mount=type=cache,target=/var/cache/apk,rw \
    apk update && apk add  \
    curl openssl bash git ca-certificates uv libstdc++

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

USER python
WORKDIR /.kagent

### STAGE 3: final
FROM python-os AS builder
ARG TOOLS_PYTHON_VERSION

WORKDIR /.kagent

ENV PATH=$PATH:/.kagent/bin:/.kagent/.venv/bin

# Copy dependency files first for better layer caching
COPY --chown=python:pythongroup pyproject.toml  .
COPY --chown=python:pythongroup .python-version .
COPY --chown=python:pythongroup uv.lock .
COPY --chown=python:pythongroup packages/kagent-adk packages/kagent-adk
COPY --chown=python:pythongroup packages/kagent-core packages/kagent-core
COPY --chown=python:pythongroup README.md .

ARG VERSION

# Install dependencies - make sure /.kagent/.venv/bin in path and not in cache mount
RUN --mount=type=cache,target=/.kagent/cache,uid=1001,gid=1001                \
    echo "Creating virtual environment and installing dependencies..."        \
    && uv venv --python=python$TOOLS_PYTHON_VERSION                           \
    && uv lock && uv sync --package kagent-adk                                \
    && uv cache prune                                                         \
    && echo "Installation complete."

ENV PATH="/.kagent/.venv/bin:$PATH"
ENV UV_PROJECT_ENVIRONMENT=/app/.venv

WORKDIR /app

ENTRYPOINT ["kagent-adk", "run", "--host", "0.0.0.0", "--port", "8080"]