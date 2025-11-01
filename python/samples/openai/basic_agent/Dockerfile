FROM python:3.13-slim

WORKDIR /app

# Install uv for faster dependency installation
RUN pip install uv

# Copy project files
COPY pyproject.toml .
COPY basic_agent/ ./basic_agent/

# Install dependencies
RUN uv pip install --system -e .

# Expose port
EXPOSE 8000

# Set environment variables (override at runtime)
ENV KAGENT_URL=http://kagent:8080
ENV PYTHONUNBUFFERED=1

# Health check
HEALTHCHECK --interval=30s --timeout=3s --start-period=5s --retries=3 \
  CMD python -c "import httpx; httpx.get('http://localhost:8000/health').raise_for_status()" || exit 1

# Run the agent
CMD ["uvicorn", "basic_agent.agent:fastapi_app", "--host", "0.0.0.0", "--port", "8000"]

