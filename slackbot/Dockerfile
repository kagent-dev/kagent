FROM python:3.13-slim

# Set environment variables
ENV PYTHONUNBUFFERED=1 \
    PYTHONDONTWRITEBYTECODE=1 \
    PIP_NO_CACHE_DIR=1 \
    PIP_DISABLE_PIP_VERSION_CHECK=1

WORKDIR /app

# Create non-root user early (files copied after will be owned by this user)
RUN useradd -m -u 1000 appuser && \
    chown appuser:appuser /app

# Switch to non-root user for subsequent operations
USER appuser

# Install dependencies first (better layer caching)
COPY --chown=appuser:appuser pyproject.toml .
RUN pip install --upgrade pip && \
    pip install .

# Copy application code
COPY --chown=appuser:appuser src/ src/

# Copy default config (can be overridden with volume mount at runtime)
COPY --chown=appuser:appuser config/ config/

# Health check
HEALTHCHECK --interval=30s --timeout=10s --start-period=5s --retries=3 \
    CMD python -c "import urllib.request; urllib.request.urlopen('http://localhost:8080/health')"

# Run application
CMD ["python", "-m", "kagent_slackbot.main"]
