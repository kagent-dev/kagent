### Stage 0: build the skills-init Go binary
ARG BASE_IMAGE_REGISTRY=cgr.dev
ARG BUILDPLATFORM
FROM --platform=$BUILDPLATFORM $BASE_IMAGE_REGISTRY/chainguard/go:latest AS builder
ARG TARGETARCH
ARG TARGETOS

WORKDIR /workspace

COPY go.mod go.sum ./
RUN --mount=type=cache,target=/root/go/pkg/mod,rw \
    --mount=type=cache,target=/root/.cache/go-build,rw \
    go mod download

COPY api/ api/
COPY core/ core/
COPY adk/ adk/

ARG LDFLAGS
RUN --mount=type=cache,target=/root/go/pkg/mod,rw \
    --mount=type=cache,target=/root/.cache/go-build,rw \
    CGO_ENABLED=0 GOOS=${TARGETOS:-linux} GOARCH=${TARGETARCH} \
    go build -a -trimpath -ldflags "$LDFLAGS" -o /skills-init ./core/cmd/skills-init

### Stage 1: runtime
FROM alpine:3.23

ARG PYTHON_UID=1001
ARG PYTHON_GID=1001

# git is invoked by skills-init via exec.Command with an argv vector — never
# through a shell — so the only attack surface here is git itself. OCI fetch
# uses the in-process go-containerregistry library, so krane and jq are gone.
RUN apk upgrade --no-cache && apk add --no-cache git openssh-client ca-certificates

COPY --from=builder /skills-init /usr/local/bin/skills-init

# Run as the same UID/GID as the main agent container (python user) so that
# files written to the shared /skills volume are readable by the main container.
# Keep these defaults aligned with the canonical main agent image definition
# (for example, python/Dockerfile) to avoid UID/GID drift across images.
RUN addgroup -g ${PYTHON_GID} pythongroup && \
    adduser -u ${PYTHON_UID} -G pythongroup -s /bin/sh -D python

USER ${PYTHON_UID}:${PYTHON_GID}

ENTRYPOINT ["/usr/local/bin/skills-init"]
