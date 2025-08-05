### STAGE 1: base image
ARG BASE_IMAGE_REGISTRY=cgr.dev
FROM $BASE_IMAGE_REGISTRY/chainguard/wolfi-base:latest AS base-os

ENV LANG=C.UTF-8
ENV LC_ALL=C.UTF-8
RUN --mount=type=cache,target=/var/cache/apk,rw \
    apk update && apk add  \
    curl openssl bash git ca-certificates uv

### STAGE 2: python
FROM base-os AS python-os
ARG TOOLS_PYTHON_VERSION=3.12

ENV PYTHONDONTWRITEBYTECODE=1
ENV PYTHONUNBUFFERED=1
ENV GIT_LFS_SKIP_SMUDGE=1

ENV UV_LINK_MODE=symlink
ENV UV_CACHE_DIR=/app/.cache/uv
ENV UV_PROJECT_ENVIRONMENT="/app/.venv"

RUN addgroup -g 10101 pythongroup                       && \
    adduser  -u 10101 -G pythongroup -s /bin/bash -D python -h /app/  && \
    mkdir    -p /app/bin                               && \
    mkdir    -p /app/.cache/uv                         && \
    chown    -vR 10101:10101 /app

USER python
WORKDIR /app

### STAGE 3: final
FROM python-os AS final

WORKDIR /app

ENV PATH=$PATH:/app/bin

# Copy kagent source code (dependency) - preserve structure
COPY --chown=python:pythongroup python/ python/

# Copy byo-adk example files - preserve structure
COPY --chown=python:pythongroup examples/ examples/

# Change to the byo-adk directory for building
WORKDIR /app/examples/byo-adk

# Install dependencies
RUN echo "Installing dependencies..."                   \
    && uv sync --locked --refresh                       \
    && uv cache prune

# Offline mode
ENV UV_OFFLINE=1

# Test if the application works
RUN uv run python main.py --help || echo "No help available, but checking if import works..." && uv run python -c "import agent; print('Agent module imported successfully')"

EXPOSE 8080
ARG VERSION

LABEL org.opencontainers.image.source=https://github.com/kagent-dev/kagent
LABEL org.opencontainers.image.description="BYO-ADK Agent example using Google ADK"
LABEL org.opencontainers.image.authors="Kagent Creators 🤖"
LABEL org.opencontainers.image.version="$VERSION"

# Make sure we're in the right directory for CMD
WORKDIR /app/examples/byo-adk
ENTRYPOINT ["uv", "run", "python", "main.py"] 