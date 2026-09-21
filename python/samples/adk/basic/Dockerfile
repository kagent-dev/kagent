### STAGE 1: base image
ARG DOCKER_REGISTRY=ghcr.io
ARG VERSION=latest
FROM $DOCKER_REGISTRY/kagent-dev/kagent/kagent-adk:$VERSION

WORKDIR /app
ENV PYTHONPATH=/app

COPY  samples/adk/basic/basic/ basic/

CMD ["basic", "--working-dir", "/app"]