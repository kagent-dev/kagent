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

### STAGE 3: final
FROM python-os AS final
ARG TOOLS_PYTHON_VERSION

WORKDIR /app/python

ENV PATH=$PATH:/app/python/bin:/app/python/.venv/bin

COPY --chown=python:pythongroup pyproject.toml  .
COPY --chown=python:pythongroup .python-version .
COPY --chown=python:pythongroup uv.lock .
COPY --chown=python:pythongroup packages packages
COPY --chown=python:pythongroup README.md .

ARG VERSION

# Install dependencies
RUN echo "Installing dependencies..."                   \
    && uv venv --python=python$TOOLS_PYTHON_VERSION     \
    && uv version ${VERSION%%-*} --package kagent       \
    && uv sync --locked --refresh                       \
    && uv build --package kagent-adk                    \
    && uv build --package kagent                        \
    && uv cache prune

# Offline mode
ENV UV_OFFLINE=1

# Test if the tool is working and fetch all dependencies
RUN kagent --help

EXPOSE 8080

LABEL org.opencontainers.image.source=https://github.com/kagent-dev/kagent
LABEL org.opencontainers.image.description="Kagent app is the apiserver for running agents."
LABEL org.opencontainers.image.authors="Kagent Creators 🤖"
LABEL org.opencontainers.image.version="$VERSION"

CMD ["kagent", "--host", "0.0.0.0", "--port", "8080"]