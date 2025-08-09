### STAGE 1: base image
FROM ghcr.io/kagent-dev/kagent/kagent-adk:v0.5.3-49-g4e168982

WORKDIR /app

COPY  basic/ basic/
COPY  pyproject.toml pyproject.toml
COPY  README.md README.md
COPY  .python-version .python-version
COPY  uv.lock uv.lock

RUN uv sync --locked --refresh

ENTRYPOINT ["kagent-adk", "run", "basic", "--host", "0.0.0.0", "--port", "8080"]