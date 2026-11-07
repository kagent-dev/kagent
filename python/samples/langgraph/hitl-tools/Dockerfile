### STAGE 1: base image
ARG DOCKER_REGISTRY=ghcr.io
ARG VERSION=latest
# substrate's gVisor (ateom) checkpoint/restore segfaults processes linked against trixie's newer glibc
# use bookworm to align with the kagent-adk runtime image
FROM python:3.13.17-slim-bookworm@sha256:e32be0e55814a08977501ca863271d9bd02443710db8f2afaf8372e2c4508530
COPY --from=ghcr.io/astral-sh/uv:0.13.5 /uv /uvx /usr/local/bin/

ENV GRPC_DEFAULT_SSL_ROOTS_FILE_PATH=/etc/ssl/certs/ca-certificates.crt

WORKDIR /app

# Install system dependencies
RUN apt-get update && apt-get install -y \
    build-essential \
    && rm -rf /var/lib/apt/lists/* \
    && apt-get clean

# this mirrors the kagent-adk's uv configuration and is needed to work on substrate
# as gVisor checkpoint/restore segfaults the base image's system
# CPython on restore and the standalone build survives. Both this and the bookworm base 
# are needed for BYO to work on substrate as this pins the base image to use a base image with glibc 2.36
# where as the prvious base image python3.13-trixie-slim used glibc 2.41
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
CMD ["uv", "run", "samples/langgraph/hitl-tools/hitl_tools/cli.py"]
