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

RUN apk upgrade --no-cache && apk add --no-cache git
COPY --from=krane-builder /build/krane /usr/local/bin/krane

# Run as the same UID/GID as the main agent container (python user) so that
# files written to the shared /skills volume are readable by the main container.
RUN addgroup -g 1001 pythongroup && \
    adduser -u 1001 -G pythongroup -s /bin/sh -D python

USER 1001:1001
