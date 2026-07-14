### STAGE 1: uv binary
ARG UV_VERSION=0.11.15
FROM ghcr.io/astral-sh/uv:${UV_VERSION} AS uv-bin

### STAGE 2: builder
# Build the uv-managed standalone Python interpreter and the project venv on a full base
# (debian-slim, digest-pinnable). Nothing from this stage ships except /python and the venv.
# Digest = multi-arch index resolved via `docker buildx imagetools inspect debian:12-slim`.
FROM debian:13-slim@sha256:28de0877c2189802884ccd20f15ee41c203573bd87bb6b883f5f46362d24c5c2 AS builder
ARG TOOLS_PYTHON_VERSION=3.13

ENV LANG=C.UTF-8
ENV LC_ALL=C.UTF-8
ENV PYTHONOPTIMIZE=2
ENV PYTHONUNBUFFERED=1

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

RUN apt-get update \
    && apt-get install -y --no-install-recommends ca-certificates \
    && rm -rf /var/lib/apt/lists/*

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
    && uv lock && uv sync --package kagent-adk --no-editable                  \
    && uv cache prune                                                         \
    && echo "Installation complete."

# Pre-create the config dir owned by the runtime user. On Agent Substrate the config is
# materialized into /config at startup (the env-injected path); distroless runs as nonroot and
# cannot create top-level dirs, so it must exist with the right owner ahead of time. On the
# normal Deployment path /config is overlaid by the mounted Secret volume, so this is harmless.
RUN mkdir -p /staging/config

### STAGE 3: final (distroless)
# distroless/cc provides glibc + libstdc++ (required by the standalone CPython build) but no
# shell or package manager. Agents that need in-container code execution / bash tools use the
# "full" image (python/Dockerfile.full) instead.
# Pinned by digest so rebuilds can't silently pull a different base. Distroless publishes no
# version-numbered tags (only :nonroot/:latest/:debug), so a digest is the only way to pin tighter
# than "Debian 12". Tag kept for readability; bump the digest via dependency tooling.
# Digest = multi-arch index resolved via `docker buildx imagetools inspect gcr.io/distroless/cc-debian12:nonroot`.
FROM gcr.io/distroless/cc-debian12:nonroot@sha256:ce0d66bc0f64aae46e6a03add867b07f42cc7b8799c949c2e898057b7f75a151
ARG VERSION

ENV LANG=C.UTF-8
ENV LC_ALL=C.UTF-8
ENV PYTHONUNBUFFERED=1
ENV PATH="/.kagent/.venv/bin:$PATH"
ENV VIRTUAL_ENV=/.kagent/.venv

# The standalone interpreter and the venv (the venv's python is linked to /python).
COPY --from=builder /python /python
COPY --from=builder /.kagent/.venv /.kagent/.venv
# Writable config dir for substrate config materialization (see builder stage).
COPY --from=builder --chown=65532:65532 /staging/config /config

# The standalone CPython build and numpy's C-extensions dynamically link a handful of system
# libraries that distroless/cc does not ship (zlib, bz2, lzma, ffi, sqlite). Copy them from the
# builder into an arch-agnostic dir on LD_LIBRARY_PATH. The *-linux-gnu glob matches the single
# multiarch dir present for the target arch (amd64/arm64).
COPY --from=builder /usr/lib/*-linux-gnu/libz.so.1 /usr/lib/kagent-libs/
COPY --from=builder /usr/lib/*-linux-gnu/libbz2.so.1* /usr/lib/kagent-libs/
COPY --from=builder /usr/lib/*-linux-gnu/liblzma.so.5* /usr/lib/kagent-libs/
COPY --from=builder /usr/lib/*-linux-gnu/libffi.so.8* /usr/lib/kagent-libs/
COPY --from=builder /usr/lib/*-linux-gnu/libsqlite3.so.0* /usr/lib/kagent-libs/
ENV LD_LIBRARY_PATH=/usr/lib/kagent-libs

WORKDIR /app
USER 65532:65532

LABEL org.opencontainers.image.source=https://github.com/kagent-dev/kagent
LABEL org.opencontainers.image.description="Kagent ADK Python runtime (distroless, no sandbox runtime)."
LABEL org.opencontainers.image.version="$VERSION"

ENTRYPOINT ["/.kagent/.venv/bin/kagent-adk", "run", "--host", "0.0.0.0", "--port", "8080"]
