### STAGE 1: base image
ARG DOCKER_REGISTRY=ghcr.io
ARG VERSION=latest
FROM ghcr.io/astral-sh/uv:python3.13-trixie-slim

WORKDIR /app

# Install system dependencies
RUN apt-get update && apt-get install -y \
    build-essential \
    curl \
    && rm -rf /var/lib/apt/lists/* \
    && apt-get clean

# Same layout as currency: context must be ./python (full uv workspace).
COPY samples samples
COPY packages packages
COPY pyproject.toml pyproject.toml
COPY README.md README.md
COPY .python-version .python-version
COPY uv.lock uv.lock

RUN uv sync --locked --no-dev \
    && uv cache clean

ENV PYTHONPATH=/app
ENV PORT=8080

EXPOSE 8080

HEALTHCHECK --interval=30s --timeout=10s --start-period=5s --retries=3 \
    CMD curl -f http://localhost:8080/health || exit 1

CMD ["uv", "run", "samples/langgraph/kebab/kebab/cli.py"]
