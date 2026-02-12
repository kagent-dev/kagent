ARG DOCKER_REGISTRY=ghcr.io
ARG VERSION=latest
FROM ghcr.io/astral-sh/uv:python3.13-trixie-slim

WORKDIR /app

RUN apt-get update && apt-get install -y \
    build-essential \
    && rm -rf /var/lib/apt/lists/* \
    && apt-get clean

COPY packages/kagent-core packages/kagent-core
COPY packages/kagent-dapr packages/kagent-dapr
COPY samples/dapr/dapr-agent samples/dapr/dapr-agent

RUN cd samples/dapr/dapr-agent \
    && uv sync --locked --prerelease=allow --no-dev \
    && uv cache clean

ENV PORT=8080

EXPOSE 8080

HEALTHCHECK --interval=30s --timeout=10s --start-period=5s --retries=3 \
    CMD curl -f http://localhost:8080/health || exit 1

WORKDIR /app/samples/dapr/dapr-agent

CMD ["uv", "run", "python", "agent.py"]
