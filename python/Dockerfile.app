ARG KAGENT_ADK_VERSION=latest
FROM ghcr.io/kagent-dev/kagent/kagent-adk:$KAGENT_ADK_VERSION

# Offline mode
ENV UV_OFFLINE=1

# Test if the tool is working and fetch all dependencies
RUN kagent-adk --help

EXPOSE 8080
ARG VERSION

LABEL org.opencontainers.image.source=https://github.com/kagent-dev/kagent
LABEL org.opencontainers.image.description="Kagent app is the apiserver for running agents."
LABEL org.opencontainers.image.authors="Kagent Creators 🤖"
LABEL org.opencontainers.image.version="$VERSION"

CMD ["kagent-adk", "--host", "0.0.0.0", "--port", "8080"]