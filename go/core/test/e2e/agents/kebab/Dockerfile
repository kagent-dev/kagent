### STAGE 1: base image
ARG DOCKER_REGISTRY=ghcr.io
ARG VERSION=latest
ARG DOCKER_REPO=kagent-dev/kagent
# Use the "-full" runtime image: it ships uv + a shell, which `uv sync` below needs.
# The default kagent-adk image is now distroless slim and has neither uv nor /bin/sh.
FROM $DOCKER_REGISTRY/$DOCKER_REPO/kagent-adk:$VERSION-full

WORKDIR /app

COPY  kebab/ kebab/
COPY  pyproject.toml pyproject.toml
COPY  README.md README.md
COPY  .python-version .python-version
COPY  uv.lock uv.lock

# Install only the kebab package into the inherited ADK venv. The base image already provides
# google-adk/kagent-adk; syncing this child project's lockfile downgrades shared runtime deps.
RUN uv pip install --python /.kagent/.venv/bin/python --no-deps .

CMD ["kebab"]