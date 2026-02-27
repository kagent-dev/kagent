### STAGE 1: base image
ARG DOCKER_REGISTRY=ghcr.io
ARG VERSION=latest
ARG DOCKER_REPO=kagent-dev/kagent
FROM $DOCKER_REGISTRY/$DOCKER_REPO/kagent-adk:$VERSION

WORKDIR /app

COPY  kebab/ kebab/
COPY  pyproject.toml pyproject.toml
COPY  README.md README.md
COPY  .python-version .python-version
COPY  uv.lock uv.lock

RUN uv sync --locked --refresh

CMD ["kebab"]