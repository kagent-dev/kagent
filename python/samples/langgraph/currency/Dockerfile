### STAGE 1: base image
ARG DOCKER_REGISTRY=ghcr.io
ARG VERSION=latest
# bookworm (glibc 2.36), NOT trixie (glibc 2.41): substrate's gVisor checkpoint/restore segfaults
# processes against trixie's newer glibc. Aligns with the kagent-adk runtime image.
FROM ghcr.io/astral-sh/uv:python3.13-bookworm-slim

WORKDIR /app

# Install system dependencies
RUN apt-get update && apt-get install -y \
    build-essential \
    && rm -rf /var/lib/apt/lists/* \
    && apt-get clean

# Use the uv-managed standalone CPython (python-build-standalone), NOT the base image's system
# Python. REQUIRED for substrate: gVisor checkpoint/restore segfaults the system CPython (a stub
# that dynamically loads libpython3.13.so) on restore; the standalone build statically embeds
# libpython and survives. Both this AND the bookworm base above are needed for BYO on substrate.
ENV UV_PYTHON_PREFERENCE=only-managed
ENV UV_PYTHON_INSTALL_DIR=/python

# Copy project files
COPY samples samples
COPY packages packages
COPY pyproject.toml pyproject.toml
COPY README.md README.md
COPY .python-version .python-version
COPY uv.lock uv.lock

# Install dependencies
RUN uv sync --locked --no-dev \
    && uv cache clean

# Set environment variables
ENV PYTHONPATH=/app
ENV PORT=8080

# Expose port
EXPOSE 8080

# Health check
HEALTHCHECK --interval=30s --timeout=10s --start-period=5s --retries=3 \
    CMD curl -f http://localhost:8080/health || exit 1

# Run the application
CMD ["uv", "run", "samples/langgraph/currency/currency/cli.py"]
