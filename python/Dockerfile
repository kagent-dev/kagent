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
ENV UV_COMPILE_BYTECODE_TIMEOUT=360
ENV UV_PROJECT_ENVIRONMENT=/.kagent/python/.venv
ENV UV_SYSTEM_PYTHON=1
ENV UV_NO_PROGRESS=1

ENV UV_CACHE_DIR=/.kagent/python/.cache/uv
ENV UV_TOOL_DIR=/.kagent/python/.cache/uv/tools
ENV UV_PYTHON_DOWNLOADS_DIR=/.kagent/python/.cache/uv/python

RUN addgroup -g 1001 pythongroup                           && \
    adduser  -u 1001 -G pythongroup -s /bin/bash -D python -h /.kagent/python/  && \
    mkdir    -p /.kagent/python/bin                        && \
    mkdir    -p /.kagent/python/downloads                  && \
    mkdir    -p /.kagent/python/.cache/uv                  && \
    chown    -vR 1001:1001 /.kagent                        && \
    chmod    755 /.kagent/python/.cache

USER python
WORKDIR /.kagent/python

### STAGE 3: final
FROM python-os AS final
ARG TOOLS_PYTHON_VERSION

WORKDIR /.kagent/python

ENV PATH=$PATH:/.kagent/python/bin:/.kagent/python/.venv/bin

# Copy dependency files first for better layer caching
COPY --chown=python:pythongroup pyproject.toml  .
COPY --chown=python:pythongroup .python-version .
COPY --chown=python:pythongroup uv.lock .
COPY --chown=python:pythongroup packages packages
COPY --chown=python:pythongroup README.md .

ARG VERSION

# Install dependencies
RUN --mount=type=cache,target=/.kagent/python/.cache/uv,uid=1001,gid=1001 \
    echo "Creating virtual environment and installing dependencies..."    \
    && uv venv --python=python$TOOLS_PYTHON_VERSION                       \
    && uv version ${VERSION%%-*} --package kagent-adk                     \
    && uv sync  --locked --refresh                                        \
    && uv build --package kagent-adk

WORKDIR /app

ENTRYPOINT ["kagent-adk", "run", "--host", "0.0.0.0", "--port", "8080"]