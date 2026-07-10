### STAGE 1: base image
ARG KAGENT_ADK_IMAGE=ghcr.io/kagent-dev/kagent/kagent-adk:latest
FROM ${KAGENT_ADK_IMAGE}

WORKDIR /app

COPY  basic/ basic/
COPY  pyproject.toml pyproject.toml
COPY  README.md README.md
COPY  .python-version .python-version
COPY  uv.lock uv.lock

# The base image already provides google-adk/kagent-adk. Do not run `uv sync` here:
# syncing this sample's lockfile into the inherited ADK venv can downgrade runtime deps.

CMD ["--working-dir", "/app", "basic"]
