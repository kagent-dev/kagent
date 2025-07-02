### STAGE 1: download-tools-cli
ARG BASE_IMAGE_REGISTRY=cgr.dev
FROM $BASE_IMAGE_REGISTRY/chainguard/wolfi-base:latest AS tools

ENV LANG=C.UTF-8
ENV LC_ALL=C.UTF-8

RUN apk update && apk add  \
    curl openssl bash git ca-certificates uv nodejs-22 npm \
    && rm -rf /var/cache/apk/*

ARG TARGETARCH
ARG TOOLS_BUN_VERSION
ARG TOOLS_HELM_VERSION
ARG TOOLS_ISTIO_VERSION
ARG TOOLS_ARGO_ROLLOUTS_VERSION
ARG TOOLS_KUBECTL_VERSION
ARG TOOLS_GRAFANA_MCP_VERSION

WORKDIR /downloads

RUN curl -LO "https://dl.k8s.io/release/v$TOOLS_KUBECTL_VERSION/bin/linux/$TARGETARCH/kubectl" \
    && chmod +x kubectl \
    && /downloads/kubectl version --client

RUN curl -L https://istio.io/downloadIstio | ISTIO_VERSION=$TOOLS_ISTIO_VERSION TARGET_ARCH=$TARGETARCH sh - \
    && mv istio-*/bin/istioctl /downloads/                                                           \
    && rm -rf istio-* \
    && /downloads/istioctl --help

# Install Helm
RUN curl -Lo helm.tar.gz https://get.helm.sh/helm-v${TOOLS_HELM_VERSION}-linux-${TARGETARCH}.tar.gz  \
    && tar -xvf helm.tar.gz                                                                             \
    && mv linux-${TARGETARCH}/helm /downloads/helm                                           \
    && chmod +x /downloads/helm \
    && /downloads/helm version

# Install kubectl-argo-rollouts
RUN curl -Lo /downloads/kubectl-argo-rollouts https://github.com/argoproj/argo-rollouts/releases/download/v${TOOLS_ARGO_ROLLOUTS_VERSION}/kubectl-argo-rollouts-linux-${TARGETARCH} \
    && chmod +x /downloads/kubectl-argo-rollouts \
    && /downloads/kubectl-argo-rollouts version

### STAGE 2: build-tools MCP
ARG BASE_IMAGE_REGISTRY=cgr.dev
FROM --platform=linux/$BUILDARCH $BASE_IMAGE_REGISTRY/chainguard/go:latest AS builder

ARG TARGETPLATFORM
ARG TARGETARCH
ARG LDFLAGS

WORKDIR /workspace

# Copy the Go Modules manifests
COPY go.mod go.mod
COPY go.sum go.sum

# cache deps before building and copying source so that we don't need to re-download as much
# and so that source changes don't invalidate our downloaded layer
RUN --mount=type=cache,target=/root/go/pkg/mod,rw      \
    --mount=type=cache,target=/root/.cache/go-build,rw \
     go mod download

# Copy the go source
COPY tools tools
COPY internal internal
# Build
# the GOARCH has not a default value to allow the binary be built according to the host where the command
# was called. For example, if we call make docker-build in a local env which has the Apple Silicon M1 SO
# the docker BUILDPLATFORM arg will be linux/arm64 when for Apple x86 it will be linux/amd64. Therefore,
# by leaving it empty we can ensure that the container and binary shipped on it will have the same platform.
RUN --mount=type=cache,target=/root/go/pkg/mod,rw         \
    --mount=type=cache,target=/root/.cache/go-build,rw    \
    CGO_ENABLED=0 GOOS=${TARGETOS:-linux} GOARCH=${TARGETARCH} go build -a -ldflags "$LDFLAGS" -o tool-server tools/cmd/tool-server/main.go

# Use distroless as minimal base image to package the manager binary
# Refer to https://github.com/GoogleContainerTools/distroless for more details
FROM gcr.io/distroless/static:nonroot

WORKDIR /
USER 65532:65532
ENV PATH=$PATH:/bin

# Copy the tools
COPY --from=tools --chown=65532:65532 /downloads/kubectl               /bin/kubectl
COPY --from=tools --chown=65532:65532 /downloads/istioctl              /bin/istioctl
COPY --from=tools --chown=65532:65532 /downloads/helm                  /bin/helm
COPY --from=tools --chown=65532:65532 /downloads/kubectl-argo-rollouts /bin/kubectl-argo-rollouts
# Copy the tool-server binary
COPY --from=builder --chown=65532:65532 /workspace/tool-server           /tool-server

ARG VERSION

LABEL org.opencontainers.image.source=https://github.com/kagent-dev/kagent
LABEL org.opencontainers.image.description="Kagent MCP tools server"
LABEL org.opencontainers.image.authors="Kagent Creators 🤖"
LABEL org.opencontainers.image.version="$VERSION"

ENTRYPOINT ["/tool-server"]