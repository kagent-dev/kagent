### Stage 0: build krane
FROM golang:1.26-alpine AS krane-builder

ENV KRANE_VERSION=v0.21.2
WORKDIR /build

RUN apk add --no-cache git && \
    git clone --depth 1 --branch $KRANE_VERSION \
    https://github.com/google/go-containerregistry.git

WORKDIR /build/go-containerregistry/cmd/krane

RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /build/krane .

FROM alpine:3.23

ARG PYTHON_UID=1001
ARG PYTHON_GID=1001

RUN apk upgrade --no-cache && apk add --no-cache git jq
COPY --from=krane-builder /build/krane /usr/local/bin/krane

# Run as the same UID/GID as the main agent container (python user) so that
# files written to the shared /skills volume are readable by the main container.
# Keep these defaults aligned with the canonical main agent image definition
# (for example, python/Dockerfile) to avoid UID/GID drift across images.
RUN addgroup -g ${PYTHON_GID} pythongroup && \
    adduser -u ${PYTHON_UID} -G pythongroup -s /bin/sh -D python

USER ${PYTHON_UID}:${PYTHON_GID}
