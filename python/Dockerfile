ARG UV_VERSION=0.12.3
ARG TOOLS_PYTHON_VERSION=3.13
ARG BASE_IMAGE_REGISTRY=cgr.dev
FROM ghcr.io/astral-sh/uv:${UV_VERSION} AS uv-bin

# Wolfi package names encode the Python major.minor, so `python-3.13` pins the
# interpreter and only its patch level floats on rebuild.
FROM ${BASE_IMAGE_REGISTRY}/chainguard/wolfi-base:latest AS builder
ARG TOOLS_PYTHON_VERSION
RUN apk add --no-cache python-${TOOLS_PYTHON_VERSION}
ENV UV_LINK_MODE=copy \
    UV_COMPILE_BYTECODE=1 \
    UV_NO_PROGRESS=1 \
    UV_PROJECT_ENVIRONMENT=/.kagent/.venv \
    UV_PYTHON_DOWNLOADS=never
COPY --from=uv-bin /uv /usr/local/bin/uv
WORKDIR /.kagent
COPY pyproject.toml .python-version uv.lock README.md ./
COPY packages/kagent-adk packages/kagent-adk
COPY packages/kagent-core packages/kagent-core
COPY packages/kagent-proto packages/kagent-proto
COPY packages/kagent-skills packages/kagent-skills
COPY packages/agentsts-adk packages/agentsts-adk
COPY packages/agentsts-core packages/agentsts-core
RUN --mount=type=cache,target=/root/.cache/uv \
    uv sync --frozen --package kagent-adk --no-editable

FROM ${BASE_IMAGE_REGISTRY}/chainguard/wolfi-base:latest
ARG VERSION
ARG TOOLS_PYTHON_VERSION
# The copied venv symlinks the interpreter, so the runtime needs the same python
# package as the builder. tzdata is required for zoneinfo lookups.
RUN apk add --no-cache python-${TOOLS_PYTHON_VERSION} bash ca-certificates tzdata \
    && mkdir /config \
    && chown 65532:65532 /config
COPY --from=builder /.kagent/.venv /.kagent/.venv
ENV LANG=C.UTF-8 \
    LC_ALL=C.UTF-8 \
    PYTHONUNBUFFERED=1 \
    PATH="/.kagent/.venv/bin:$PATH" \
    VIRTUAL_ENV=/.kagent/.venv
WORKDIR /app
USER 65532:65532
LABEL org.opencontainers.image.source=https://github.com/kagent-dev/kagent \
      org.opencontainers.image.description="Kagent ADK Python runtime." \
      org.opencontainers.image.version="$VERSION"
ENTRYPOINT ["/.kagent/.venv/bin/kagent-adk", "run", "--host", "0.0.0.0", "--port", "8080"]
