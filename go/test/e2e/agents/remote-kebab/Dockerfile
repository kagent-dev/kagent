FROM python:3.13-slim

WORKDIR /app

RUN apt-get update && apt-get install -y \
    && rm -rf /var/lib/apt/lists/*

# Install uv
RUN pip install uv

# Copy dependency files
COPY pyproject.toml .
COPY uv.lock .

# Install Python dependencies
RUN uv pip install --system -e .

# Copy application code
COPY agent.py .

EXPOSE 8080

CMD ["uvicorn", "agent:a2a_app", "--host", "0.0.0.0", "--port", "8080"]
