### Stage 0: build krane
ARG BASE_IMAGE_REGISTRY=cgr.dev
FROM $BASE_IMAGE_REGISTRY/chainguard/go:latest AS krane-builder

ENV KRANE_VERSION=v0.20.7
WORKDIR /build

RUN git clone --depth 1 --branch $KRANE_VERSION \
    https://github.com/google/go-containerregistry.git

WORKDIR /build/go-containerregistry/cmd/krane

RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /build/krane .

### Stage 1: final image
ARG BASE_IMAGE_REGISTRY=cgr.dev
FROM $BASE_IMAGE_REGISTRY/chainguard/wolfi-base:latest

RUN apk add --no-cache git
COPY --from=krane-builder /build/krane /usr/local/bin/krane
