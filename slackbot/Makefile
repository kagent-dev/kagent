.PHONY: help build push dev test lint type-check clean install

# Default target
help:
	@echo "Kagent Slackbot - Available targets:"
	@echo "  make install     - Install Python dependencies"
	@echo "  make dev         - Run bot locally"
	@echo "  make test        - Run tests (if available)"
	@echo "  make lint        - Run linting with ruff"
	@echo "  make type-check  - Run type checking with mypy"
	@echo "  make build       - Build Docker image"
	@echo "  make push        - Push Docker image to registry"
	@echo "  make clean       - Remove build artifacts"

# Configuration
IMAGE_REGISTRY ?= kagent-dev/kagent
IMAGE_NAME ?= slackbot
IMAGE_TAG ?= latest
IMAGE_FULL = $(IMAGE_REGISTRY)/$(IMAGE_NAME):$(IMAGE_TAG)

# Install dependencies
install:
	python3 -m venv .venv
	.venv/bin/pip install --upgrade pip
	.venv/bin/pip install -e ".[dev]"

# Run locally
dev:
	@if [ ! -f .env ]; then \
		echo "Error: .env file not found. Copy .env.example and fill in your Slack tokens."; \
		exit 1; \
	fi
	.venv/bin/python -m kagent_slackbot.main

# Run tests (placeholder - add tests later)
test:
	@echo "No tests implemented yet"
	# .venv/bin/pytest tests/

# Lint with ruff
lint:
	.venv/bin/ruff check src/

# Lint and auto-fix
lint-fix:
	.venv/bin/ruff check --fix src/

# Type check with mypy
type-check:
	.venv/bin/mypy src/kagent_slackbot/

# Build Docker image
build:
	@echo "Building Docker image: $(IMAGE_FULL)"
	docker build -t $(IMAGE_FULL) .

# Push to registry
push: build
	@echo "Pushing Docker image: $(IMAGE_FULL)"
	docker push $(IMAGE_FULL)

# Clean build artifacts
clean:
	rm -rf .venv
	rm -rf __pycache__
	find . -type d -name "__pycache__" -exec rm -rf {} + 2>/dev/null || true
	find . -type f -name "*.pyc" -delete
	find . -type d -name "*.egg-info" -exec rm -rf {} + 2>/dev/null || true
