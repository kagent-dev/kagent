### STAGE 1: base image
FROM ghcr.io/astral-sh/uv:python3.12-bookworm-slim

WORKDIR /app

# Install system dependencies
RUN apt-get update && apt-get install -y \
    build-essential \
    && rm -rf /var/lib/apt/lists/*

# Copy project files
COPY pyproject.toml pyproject.toml
COPY packages packages
COPY samples/openai/basic_agent/ ./samples/openai/basic_agent/
COPY README.md README.md
COPY uv.lock uv.lock

# Install dependencies
RUN uv venv && uv sync --no-dev --package basic-openai-agent

# Expose port
EXPOSE 80
ENV PORT=80
ENV VIRTUAL_ENV=/app/.venv
ENV PATH="/app/.venv/bin:$PATH"

# Health check
HEALTHCHECK --interval=30s --timeout=3s --start-period=5s --retries=3 \
  CMD curl -f http://localhost:${PORT}/health || exit 1

# Run the agent
CMD ["python", "samples/openai/basic_agent/basic_agent/agent.py"]
