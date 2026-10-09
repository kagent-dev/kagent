#!/usr/bin/env bash
# Stand up a kagent dev cluster from nothing, on this machine (arm64).
#
# `make kagent-cli-deploy` installs Substrate, the development database, and kagent
# from this checkout; the rest leaves an agent to talk to and the forwards a developer
# needs. README.md beside this file is the reader's version of the same thing.
set -euo pipefail

# The repo this script lives in, so it works from any checkout and any directory.
REPO="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "$REPO"

step() { printf '\n\033[1;36m==> %s\033[0m\n' "$1"; }

step "1/5  Kind cluster and local registry on :5001"
make create-kind-cluster

step "2/5  Images and the kagent install, all built from this checkout"
make build kagent-cli-deploy

step "3/5  The agent runtime image, pinned by digest"
# Built for this machine's own architecture: the image runs on the Kind node, which is
# a container on this host, so a cross-built one would not start.
ARCH="$(uname -m)"; [ "$ARCH" = "x86_64" ] && ARCH=amd64; [ "$ARCH" = "aarch64" ] && ARCH=arm64
# The Go ADK, which is what the chart names as the image for declarative agents, and
# the one that survives being an actor: an actor starts by restoring the template's
# golden snapshot, and the Python runtime dies on restore with SIGILL where a static
# Go binary comes back. Substrate requires a digest, and only a registry can give one,
# so it is pushed rather than loaded.
docker buildx build --push --platform "linux/${ARCH}" \
  --build-arg BASE_IMAGE_REGISTRY=cgr.dev \
  --build-arg BUILD_PACKAGE=adk/cmd/main.go \
  -t localhost:5001/kagent-dev/kagent/golang-adk:dev -f go/Dockerfile ./go
HARNESS_DIGEST="$(docker buildx imagetools inspect localhost:5001/kagent-dev/kagent/golang-adk:dev \
  | awk '/^Digest:/{print $2}')"

step "4/5  An Agent with inline template and Harness"
kubectl apply -f - <<EOF
apiVersion: api.kagent.dev/v1alpha3
kind: Agent
metadata:
  name: assistant
  namespace: kagent
spec:
  template:
    modelConfig:
      name: default-model-config
    description: A general-purpose assistant.
    systemPrompt: You are a helpful assistant running on kagent.
  harness:
    kagent: {}
    workload:
      image: localhost:5001/kagent-dev/kagent/golang-adk@${HARNESS_DIGEST}
    substrate:
      workerPoolRef:
        name: kagent-default
      snapshotPolicy:
        location: s3://ate-snapshots/kagent
EOF

# Ready means Substrate has booted the template's golden actor and snapshotted it, which
# takes a minute or so. Waited for here rather than left to the reader, because an agent
# that is not ready yet is listed but refuses conversations, and nothing on the page
# explains that it is a matter of waiting.
printf 'waiting for the agent to become ready'
for _ in $(seq 1 40); do
  ready="$(kubectl get agent -n kagent assistant \
    -o jsonpath='{.status.conditions[?(@.type=="Ready")].status}' 2>/dev/null || true)"
  [ "$ready" = "True" ] && break
  printf '.'; sleep 15
done
echo
kubectl get agent -n kagent assistant \
  -o jsonpath='agent assistant x kagent: Ready={.status.conditions[?(@.type=="Ready")].status}{"\n"}'

step "5/5  Done"
kubectl get pods -n kagent

# Both forwards a developer needs, held open together.
#
# 8080 is the UI the cluster is running -- the image built above, served by its nginx,
# not a dev server. 8083 is the controller, which `yarn dev` proxies to by default.
#
# The second one is here so that default is true. With only the UI forwarded, running
# the dev server needed KAGENT_UI_DEV_CONTROLLER_URL pointed at 8080 in `ui/.env`, and
# without that line every read failed with `ECONNREFUSED 127.0.0.1:8083` on a page that
# otherwise loaded -- which reads as a broken backend rather than a missing forward. A
# second `kubectl` is cheaper than a setting every reader has to be told about.
#
# Backgrounded and waited on rather than `exec`ed, because two of them cannot both be
# the foreground process. The trap is what makes Ctrl-C take both down: without it the
# script would exit and leave orphaned forwards holding the ports, and the next run
# would fail on an address already in use.
kubectl -n kagent port-forward svc/kagent-ui 8080:8080 &
UI_FORWARD=$!
kubectl -n kagent port-forward svc/kagent-controller 8083:8083 &
CONTROLLER_FORWARD=$!
trap 'kill "$UI_FORWARD" "$CONTROLLER_FORWARD" 2>/dev/null || true' EXIT INT TERM

printf '\n\033[1;32m==> The UI is at http://localhost:8080\033[0m\n'
printf '    The controller is on :8083, which `cd ui && yarn dev` uses by default.\n'
printf '    (both port-forwards running in this shell; Ctrl-C to stop)\n\n'

# Held until either one dies, because a forward that has gone means whatever depended on
# it is now failing and saying so beats leaving one working and one silently absent.
#
# Polled rather than `wait -n`, which is bash 4.3 and this is a script for a Mac: the
# system bash here is 3.2, where `wait -n` is a syntax error and `wait` alone would sit
# on a dead forward until the other one went too.
while kill -0 "$UI_FORWARD" 2>/dev/null && kill -0 "$CONTROLLER_FORWARD" 2>/dev/null; do
  sleep 1
done
