### STAGE 1: base image
ARG DOCKER_REGISTRY=ghcr.io
ARG VERSION=latest
FROM ghcr.io/astral-sh/uv:python3.13-trixie-slim

WORKDIR /app

# Install system dependencies
RUN apt-get update && apt-get install -y \
    build-essential \
    && rm -rf /var/lib/apt/lists/*

# Copy project files
COPY pyproject.toml .
COPY basic_agent/ ./basic_agent/
COPY README.md README.md
COPY .python-version .python-version
COPY uv.lock uv.lock

# Install dependencies
RUN uv sync --locked --no-dev

# Expose port
EXPOSE 8000

# Set environment variables (override at runtime)
ENV KAGENT_URL=http://kagent:8080
ENV PYTHONUNBUFFERED=1

# Health check
HEALTHCHECK --interval=30s --timeout=3s --start-period=5s --retries=3 \
  CMD curl -f http://localhost:8000/health || exit 1

# Run the agent
CMD ["uv", "run", "basic_agent/agent.py"]

