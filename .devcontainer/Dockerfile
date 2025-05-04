ARG TOOLS_UV_VERSION=latest
ARG TOOLS_NODE_VERSION=latest
ARG ARCH=${TARGETARCH:-amd64}

#STAGE 1 - base image
FROM --platform=$BUILDPLATFORM ghcr.io/astral-sh/uv:${TOOLS_UV_VERSION}-debian-slim AS tools-uv
FROM --platform=$BUILDPLATFORM node:${TOOLS_NODE_VERSION}-bookworm AS node-base

RUN apt-get update && apt-get install -y --no-install-recommends \
    wget                      \
    git                       \
    zsh                       \
    jq                        \
    build-essential           \
    curl                      \
    ca-certificates           \
    python3                   \
    python3-pip               \
    python3-venv              \
    python3-dev               \
    libffi-dev                \
    libssl-dev                \
    && apt-get update         \
    && update-ca-certificates \
    && curl -fsSL https://get.docker.com -o get-docker.sh  \
    && sh get-docker.sh  \
    && apt-get clean          \
    && rm -rf /var/log/*log   \
    && rm -rf /var/lib/apt/lists/*

### STAGE 2 add golang
FROM --platform=$BUILDPLATFORM node-base AS node-python-golang
ARG TOOLS_GO_VERSION
ARG TOOLS_KIND_VERSION
ARG TOOLS_ISTIO_VERSION
ARG TOOLS_ARGO_CD_VERSION
ARG ARCH

#ENVIRONMENT VARIABLES
ENV DEBIAN_FRONTEND=noninteractive
ENV GO111MODULE=on
ENV GOTOOLCHAIN=local
ENV GOPROXY="http://proxy.golang.org"
ENV PATH=$PATH:/usr/local/go/bin

RUN --mount=type=cache,target=/cache,rw \
    wget -q --show-progress -P /cache https://golang.org/dl/go${TOOLS_GO_VERSION}.linux-${ARCH}.tar.gz && \
    tar     -C /usr/local -xzf /cache/go${TOOLS_GO_VERSION}.linux-${ARCH}.tar.gz

### STAGE 3 add tools
FROM --platform=$BUILDPLATFORM node-python-golang AS tools-node-python-golang

# Install kubectl
RUN curl -LO "https://dl.k8s.io/release/${TOOLS_KUBECTL_VERSION}/bin/$BUILDPLATFORM/kubectl" \
    && chmod +x kubectl \
    && mv kubectl /usr/local/bin/

# Install kubectl argo plugin
RUN curl -o /usr/local/bin/kubectl-argo-rollouts -L https://github.com/argoproj/argo-rollouts/releases/${TOOLS_ARGO_CD_VERSION}/download/kubectl-argo-rollouts-linux-${ARCH} \
    && chmod +x /usr/local/bin/kubectl-argo-rollouts

# Install Istio
RUN curl -L https://istio.io/downloadIstio | TOOLS_ISTIO_VERSION=$TOOLS_ISTIO_VERSION TARGET_ARCH=${ARCH} sh - \
    && mv istio-$TOOLS_ISTIO_VERSION/bin/istioctl /usr/local/bin/istioctl \
    && rm -rf istio-$TOOLS_ISTIO_VERSION

# Install kind
RUN curl -Lo ./kind https://kind.sigs.k8s.io/dl/v0.27.0/kind-$(uname)-${ARCH} \
    && chmod +x ./kind \
    && mv ./kind /usr/local/bin/

#Install Helm
RUN curl -fsSL -o get_helm.sh https://raw.githubusercontent.com/helm/helm/main/scripts/get-helm-3 \
    && chmod 700 get_helm.sh \
    && ./get_helm.sh \
    && rm get_helm.sh

RUN sh -c "$(wget https://raw.github.com/robbyrussell/oh-my-zsh/master/tools/install.sh -O -)" \
    sh -c "$(curl -fsSL https://starship.rs/install.sh)" -- --yes \
    && mkdir -p /root/.config

ENTRYPOINT ["zsh"]