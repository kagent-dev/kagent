FROM python:3.11-slim

WORKDIR /app

COPY requirements.txt .
RUN pip install --no-cache-dir -r requirements.txt

COPY server.py .

# Expose port if running SSE (default 8000 if configured)
EXPOSE 8000

# Default to running the server (stdio mode by default for FastMCP)
CMD ["python", "server.py"]
