### Stage 0: build krane
FROM golang:1.25-alpine AS krane-builder

ENV KRANE_VERSION=v0.20.7
WORKDIR /build

RUN apk add --no-cache git && \
    git clone --depth 1 --branch $KRANE_VERSION \
    https://github.com/google/go-containerregistry.git

WORKDIR /build/go-containerregistry/cmd/krane

RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /build/krane .

FROM alpine:3.21

RUN apk add --no-cache git
COPY --from=krane-builder /build/krane /usr/local/bin/krane
