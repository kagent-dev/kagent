# kagent ACP sandbox image family.
#
# Layout:
#   Stage "builder"  — compiles the acp-shim static binary from this repo.
#   Stage "base"     — minimal runtime + acp-shim entrypoint. Published as
#                      ghcr.io/kagent-dev/kagent/acp-sandbox-base. Contains
#                      NO agent: it is the uniform transport layer.
#   Agent targets    — thin layers that install one stdio ACP agent and set
#                      its launch command. Build with --target <agent>:
#
#       docker build -f docker/acp-sandbox/Dockerfile --target hermes   go/
#       docker build -f docker/acp-sandbox/Dockerfile --target openclaw go/
#
# The contract between base and agent layers is intentionally tiny:
#   - ENTRYPOINT is acp-shim; it serves ws://0.0.0.0:9000/acp.
#   - The agent layer provides the child command either as CMD args after
#     "--" or via the ACP_SHIM_CHILD env var.
#
# OpenClaw note: NemoClaw's sandbox-base is built from the NemoClaw repo
# for a different runtime, so we don't extend it. The "openclaw" target below installs
# the OpenClaw CLI directly on the kagent base (same npm install NemoClaw's
# Dockerfile.base uses) and runs its own gateway alongside the shim.

### Stage 0: build the acp-shim Go binary
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
    go build -a -trimpath -ldflags "$LDFLAGS" -o /acp-shim ./core/cmd/acp-shim

### Stage 1: kagent-owned sandbox base — shim only, no agent.
FROM debian:trixie-slim AS base
ENV DEBIAN_FRONTEND=noninteractive
RUN apt-get update && apt-get install -y --no-install-recommends \
        ca-certificates \
        curl \
        git \
    && rm -rf /var/lib/apt/lists/*

# Single unprivileged user. Substrate provides the isolation boundary
# (microVM); we don't need a separate gateway/sandbox user split here.
RUN groupadd -r agent && useradd -r -m -g agent -d /home/agent -s /bin/bash agent

COPY --from=builder /acp-shim /usr/local/bin/acp-shim

USER agent
WORKDIR /home/agent
EXPOSE 9000

ENTRYPOINT ["/usr/local/bin/acp-shim"]
# Agent layers append the child command, e.g.:
#   CMD ["--", "hermes", "acp"]

### Shared: base + Node.js 22 for npm-distributed agents. Debian trixie's
### apt nodejs is v20, below OpenClaw's >=22.19 requirement (NemoClaw's
### sandbox-base uses node:22-trixie-slim for the same reason), so take
### the runtime from the official image.
FROM node:22-trixie-slim AS node
FROM base AS node-base
USER root
COPY --from=node /usr/local/bin/node /usr/local/bin/node
COPY --from=node /usr/local/lib/node_modules /usr/local/lib/node_modules
RUN ln -s ../lib/node_modules/npm/bin/npm-cli.js /usr/local/bin/npm \
    && ln -s ../lib/node_modules/npm/bin/npx-cli.js /usr/local/bin/npx \
    && node --version && npm --version
USER agent

### Agent target: Hermes (pip)
FROM base AS hermes
USER root
# xz-utils: hermes extracts .tar.xz payloads (tools/skills) on first run; without
# it the child logs "tar (child): xz: Cannot exec". python3*: hermes runtime + pip.
RUN apt-get update && apt-get install -y --no-install-recommends python3 python3-pip python3-venv xz-utils \
    && rm -rf /var/lib/apt/lists/*
# Persistent Hermes messaging gateway launcher (Slack/Telegram/...). See the
# script header for the Substrate checkpoint/restore rationale.
COPY --chmod=755 core/cmd/acp-shim/sandbox/hermes-gateway-ensure.sh /usr/local/bin/hermes-gateway-ensure.sh
USER agent
# [acp,all] pulls the ACP bridge + Slack/etc. extras. NOTE: hermes lazy-installs
# each messaging platform's runtime lib on first `gateway run` via tools.lazy_deps
# (e.g. python-telegram-bot for Telegram) — [all] does NOT pre-install them. Under
# Substrate the gateway is a backgrounded process inside a checkpoint/restored
# actor with a narrow warm window, so a 15-20s runtime PyPI install means the
# gateway is almost never ready when a Telegram message arrives. Pre-install the
# Telegram platform lib at build time so `gateway run` connects in ~2s and needs
# no PyPI egress. Pin matches hermes 0.16.0's lazy_deps target (python-telegram-bot[webhooks]==22.6).
RUN python3 -m venv /home/agent/.venv \
    && /home/agent/.venv/bin/pip install --no-cache-dir "hermes-agent[acp,all]" \
    && /home/agent/.venv/bin/pip install --no-cache-dir "python-telegram-bot[webhooks]==22.6"
ENV PATH="/home/agent/.venv/bin:${PATH}"
# Hermes ACP sessions are in-memory: the child MUST be long-lived (default)
# so session/load works across bridge reconnects.
CMD ["--", "hermes", "acp"]

### Agent target: OpenClaw — built on the kagent base, NOT on NemoClaw's
### sandbox-base (that image is built from the NemoClaw repo and carries
### NemoClaw-specific contracts — gateway/sandbox users, gosu, .openclaw
### tree — that we don't want to depend on). We install the OpenClaw CLI
### the same way NemoClaw's Dockerfile.base does: npm install -g.
###
### `openclaw acp` is a stdio→WS bridge that needs a sandbox-local gateway,
### so this target uses launcher scripts that start `openclaw gateway` in the
### background before the shim takes over. The launchers live in
### core/cmd/acp-shim/sandbox/ (co-located with the shim they wrap); see each
### script's header for the per-script rationale.
FROM node-base AS openclaw
ARG OPENCLAW_VERSION=2026.5.27
USER root
RUN npm install -g "openclaw@${OPENCLAW_VERSION}"
COPY --chmod=755 \
    core/cmd/acp-shim/sandbox/openclaw-gateway-ensure.sh \
    core/cmd/acp-shim/sandbox/openclaw-acp-child.sh \
    core/cmd/acp-shim/sandbox/openclaw-acp-entrypoint.sh \
    /usr/local/bin/
USER agent
ENTRYPOINT ["/usr/local/bin/openclaw-acp-entrypoint.sh"]
CMD []

