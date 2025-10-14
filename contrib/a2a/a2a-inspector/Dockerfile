#https://github.com/a2aproject/a2a-inspector

# Stage 1: Build the frontend assets
FROM node:18-alpine AS builder

RUN apk add git

# clone the repository https://github.com/a2aproject/a2a-inspector.git

WORKDIR /app
RUN git clone https://github.com/a2aproject/a2a-inspector.git

WORKDIR /app/a2a-inspector/frontend

# install dependencies
RUN npm install

# build the frontend assets
RUN npm rebuild esbuild

RUN npm run build

# Stage 2: Build the final application with the backend
FROM python:3.13-slim

WORKDIR /app

# Install build dependencies for Python packages
RUN apt-get update && apt-get install -y --no-install-recommends \
    gcc \
    libc6-dev \
    libffi-dev \
    && rm -rf /var/lib/apt/lists/*

RUN pip install uv
RUN mkdir -p /app/frontend

COPY --from=builder /app/a2a-inspector/pyproject.toml /app/a2a-inspector/uv.lock ./
COPY --from=builder /app/a2a-inspector/backend/ /app/backend/
COPY --from=builder /app/a2a-inspector/frontend/public   /app/frontend/public

RUN uv sync --no-cache
RUN uv pip install validators

WORKDIR /app/backend

# Expose the port the backend server runs on
EXPOSE 8080

# The command to run the server. Because our WORKDIR is now /app/backend,
# we can simply refer to 'app:app' from the current directory.
CMD ["uv", "run", "--", "uvicorn", "app:app", "--host", "0.0.0.0", "--port", "8080"]