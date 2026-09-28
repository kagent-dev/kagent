ARG UV_VERSION=0.12.3
FROM ghcr.io/astral-sh/uv:${UV_VERSION} AS uv-bin

# Match the BYO images' glibc version for Substrate checkpoint/restore.
FROM debian:bookworm-slim AS base
ENV UV_PYTHON_INSTALL_DIR=/python
COPY --from=uv-bin /uv /usr/local/bin/uv
RUN apt-get update \
    && apt-get install -y --no-install-recommends bash ca-certificates libstdc++6 \
    && rm -rf /var/lib/apt/lists/*

FROM base AS builder
ARG TOOLS_PYTHON_VERSION=3.13
ENV UV_LINK_MODE=copy \
    UV_COMPILE_BYTECODE=1 \
    UV_NO_PROGRESS=1 \
    UV_PROJECT_ENVIRONMENT=/.kagent/.venv \
    UV_PYTHON_PREFERENCE=only-managed
WORKDIR /.kagent
COPY pyproject.toml .python-version uv.lock README.md ./
COPY packages/kagent-adk packages/kagent-adk
COPY packages/kagent-core packages/kagent-core
COPY packages/kagent-proto packages/kagent-proto
COPY packages/kagent-skills packages/kagent-skills
COPY packages/agentsts-adk packages/agentsts-adk
COPY packages/agentsts-core packages/agentsts-core
RUN --mount=type=cache,target=/root/.cache/uv \
    uv sync --frozen --package kagent-adk --no-editable --no-dev --python "$TOOLS_PYTHON_VERSION" \
    # The standalone interpreter bundles pip; uv handles package installation.
    && uv pip uninstall --system --break-system-packages pip \
    && rm -rf /python/cpython-*/lib/python*/ensurepip

FROM base
ARG VERSION
RUN mkdir /config \
    && chown 65532:65532 /config
COPY --from=builder /python /python
COPY --from=builder /.kagent/.venv /.kagent/.venv
ENV LANG=C.UTF-8 \
    LC_ALL=C.UTF-8 \
    PYTHONUNBUFFERED=1 \
    UV_PYTHON_DOWNLOADS=never \
    UV_CACHE_DIR=/config/.cache/uv \
    PATH="/.kagent/.venv/bin:$PATH" \
    VIRTUAL_ENV=/.kagent/.venv
WORKDIR /app
USER 65532:65532
LABEL org.opencontainers.image.source=https://github.com/kagent-dev/kagent \
      org.opencontainers.image.description="Kagent ADK Python runtime." \
      org.opencontainers.image.version="$VERSION"
ENTRYPOINT ["/.kagent/.venv/bin/kagent-adk", "run", "--host", "0.0.0.0", "--port", "8080"]
