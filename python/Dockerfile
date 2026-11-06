### STAGE 1: uv binary
ARG BASE_IMAGE_REGISTRY=cgr.dev
ARG UV_VERSION=0.13.7
FROM ghcr.io/astral-sh/uv:${UV_VERSION} AS uv-bin

### STAGE 2: builder
# Build the uv-managed standalone Python interpreter and the project venv. Nothing
# from this stage ships except /python and the venv.
FROM ${BASE_IMAGE_REGISTRY}/chainguard/wolfi-base:latest@sha256:8509ee4351a2e501b7f98201f2e2642396e82020c61a589cacdcfe74b9de46c1 AS builder
ARG TOOLS_PYTHON_VERSION=3.13

ENV LANG=C.UTF-8
ENV LC_ALL=C.UTF-8
ENV PYTHONOPTIMIZE=2
ENV PYTHONUNBUFFERED=1
ENV GRPC_DEFAULT_SSL_ROOTS_FILE_PATH=/etc/ssl/certs/ca-certificates.crt

# uv configuration: install a managed standalone Python under /python and a copy-mode venv
# (no editable installs) so the final image is self-contained with no source tree dependency.
ENV UV_LINK_MODE=copy
ENV UV_COMPILE_BYTECODE=1
ENV UV_NO_PROGRESS=1
ENV UV_HTTP_TIMEOUT=60
ENV UV_CACHE_DIR=/.kagent/cache/packages
ENV UV_PYTHON_DOWNLOADS_DIR=/.kagent/cache/downloads
ENV UV_PROJECT_ENVIRONMENT=/.kagent/.venv
ENV UV_PYTHON_INSTALL_DIR=/python
ENV UV_PYTHON_PREFERENCE=only-managed

RUN apk add --no-cache "ca-certificates>=20260909-r3"

COPY --from=uv-bin /uv /uvx /usr/local/bin/

WORKDIR /.kagent

# Copy dependency files first for better layer caching
COPY pyproject.toml .
COPY .python-version .
COPY uv.lock .
COPY README.md .
COPY packages/kagent-adk packages/kagent-adk
COPY packages/kagent-core packages/kagent-core
COPY packages/kagent-skills packages/kagent-skills
COPY packages/agentsts-adk packages/agentsts-adk
COPY packages/agentsts-core packages/agentsts-core

ARG VERSION

# Create the venv and install kagent-adk. --no-editable copies the workspace packages into
# site-packages so the runtime does not need the source tree.
RUN --mount=type=cache,target=/.kagent/cache,rw                               \
    echo "Creating virtual environment and installing dependencies..."        \
    && uv venv --python=python$TOOLS_PYTHON_VERSION                           \
    && uv sync --frozen --package kagent-adk --no-editable                    \
    && uv cache prune                                                         \
    # The standalone CPython build bundles pip (with vendored msgpack /
    # pkg_resources) in its own site-packages. The runtime only uses the venv,
    # so drop it rather than shipping CVE-flagged packages that nothing imports.
    && rm -rf /python/cpython-*/lib/python*/site-packages/pip                 \
              /python/cpython-*/lib/python*/site-packages/pip-*.dist-info     \
              /python/cpython-*/lib/python*/site-packages/setuptools          \
              /python/cpython-*/lib/python*/site-packages/setuptools-*.dist-info \
              /python/cpython-*/lib/python*/site-packages/pkg_resources      \
    # Fail the build if pip survived (rm -rf on a non-matching glob exits 0,
    # e.g. after a Python minor bump moves site-packages). --system skips venv
    # discovery so this probes the standalone interpreter.
    && ! "$(uv python find --system)" -c "import pip" 2>/dev/null             \
    && echo "Installation complete."

# Pre-create the config dir owned by the runtime user. On Agent Substrate the config is
# materialized into /config at startup (the env-injected path); the runtime runs as nonroot and
# cannot create top-level dirs, so it must exist with the right owner ahead of time. On the
# normal Deployment path /config is overlaid by the mounted Secret volume, so this is harmless.
RUN mkdir -p /staging/config

### STAGE 3: final
# Agents that need in-container code execution / bash tools use the "full" image
# (python/Dockerfile.full) instead.
FROM ${BASE_IMAGE_REGISTRY}/chainguard/wolfi-base:latest@sha256:8509ee4351a2e501b7f98201f2e2642396e82020c61a589cacdcfe74b9de46c1
ARG VERSION

# Shared libraries the venv's C extensions link against (libstdc++ for pyarrow and friends,
# zlib for numpy). The standalone CPython build bundles its own sqlite, bz2, lzma and ffi.
# tzdata backs zoneinfo lookups.
RUN apk add --no-cache "ca-certificates>=20260909-r3" libstdc++ zlib tzdata

ENV LANG=C.UTF-8
ENV LC_ALL=C.UTF-8
ENV PYTHONUNBUFFERED=1
ENV GRPC_DEFAULT_SSL_ROOTS_FILE_PATH=/etc/ssl/certs/ca-certificates.crt
ENV PATH="/.kagent/.venv/bin:$PATH"
ENV VIRTUAL_ENV=/.kagent/.venv

# The standalone interpreter and the venv (the venv's python is linked to /python).
COPY --from=builder /python /python
COPY --from=builder /.kagent/.venv /.kagent/.venv
# Writable config dir for substrate config materialization (see builder stage).
COPY --from=builder --chown=65532:65532 /staging/config /config

WORKDIR /app
USER 65532:65532

LABEL org.opencontainers.image.source=https://github.com/kagent-dev/kagent
LABEL org.opencontainers.image.description="Kagent ADK Python runtime (no sandbox runtime)."
LABEL org.opencontainers.image.version="$VERSION"

ENTRYPOINT ["/.kagent/.venv/bin/kagent-adk", "run", "--host", "0.0.0.0", "--port", "8080"]
