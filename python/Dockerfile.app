ARG KAGENT_ADK_VERSION=latest
ARG DOCKER_REGISTRY=ghcr.io
ARG DOCKER_REPO=kagent-dev/kagent
FROM $DOCKER_REGISTRY/$DOCKER_REPO/kagent-adk:$KAGENT_ADK_VERSION

# Offline mode
ENV UV_OFFLINE=1

EXPOSE 8080
ARG VERSION

LABEL org.opencontainers.image.source=https://github.com/kagent-dev/kagent
LABEL org.opencontainers.image.description="Kagent app is the Kagent agent runtime for adk agents."
LABEL org.opencontainers.image.authors="Kagent Creators 🤖"
LABEL org.opencontainers.image.version="$VERSION"

CMD ["kagent-adk", "static", "--host", "0.0.0.0", "--port", "8080"]