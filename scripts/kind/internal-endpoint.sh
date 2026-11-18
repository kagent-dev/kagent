#!/usr/bin/env bash

# Prints "--internal" when kind kubeconfig exports should use Kind's internal
# endpoint: the caller opted in via KIND_USE_INTERNAL_ENDPOINT=true AND this
# container is visible to the Docker daemon (so it can join the Docker "kind"
# network). Codespaces-style setups share the daemon's network namespace, so
# the default loopback endpoint already works there, the container is not
# visible to the daemon under its hostname, and nothing is printed.
#
# Shared by scripts/kind/setup-kind.sh and the use-kind-cluster Makefile
# target so the two cannot disagree about which endpoint a kubeconfig uses.

set -o errexit
set -o pipefail
set -o nounset

if [ "${KIND_USE_INTERNAL_ENDPOINT:-false}" = true ]; then
  if docker inspect --type container "$(hostname)" >/dev/null 2>&1; then
    echo --internal
  fi
fi
