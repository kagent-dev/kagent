#!/usr/bin/env bash

set -o errexit
set -o pipefail

KIND_CLUSTER_NAME=${KIND_CLUSTER_NAME:-kagent}
KIND_IMAGE_VERSION=${KIND_IMAGE_VERSION:-1.35.0}

# Auto-detect container runtime: prefer CONTAINER_RUNTIME env var,
# fall back to podman if available, then docker.
CONTAINER_RUNTIME=${CONTAINER_RUNTIME:-$(command -v podman >/dev/null 2>&1 && echo podman || echo docker)}

# 1. Create registry container unless it already exists
# Override REG_NAME / REG_PORT / REG_SCHEME to reuse an existing local registry
# (e.g. an HTTPS registry on another port) instead of creating a fresh kind-registry.
reg_name="${REG_NAME:-kind-registry}"
reg_port="${REG_PORT:-5001}"
reg_scheme="${REG_SCHEME:-http}"
if [ "$("${CONTAINER_RUNTIME}" inspect -f '{{.State.Running}}' "${reg_name}" 2>/dev/null || true)" != 'true' ]; then
  "${CONTAINER_RUNTIME}" run \
    -d --restart=always -p "127.0.0.1:${reg_port}:5000" --network bridge --name "${reg_name}" \
    registry:2
fi

# 2. Create kind cluster with containerd registry config dir enabled
if kind get clusters | grep -qx "${KIND_CLUSTER_NAME}"; then
  echo "Kind cluster '${KIND_CLUSTER_NAME}' already exists; skipping create."
else
  # When using podman, set the KIND_EXPERIMENTAL_PROVIDER
  export KIND_EXPERIMENTAL_PROVIDER="${CONTAINER_RUNTIME}"
  kind create cluster --name "${KIND_CLUSTER_NAME}" \
    --config scripts/kind/kind-config.yaml \
    --image="kindest/node:v${KIND_IMAGE_VERSION}"
fi

# 3. Add the registry config to the nodes
#
# This is necessary because localhost resolves to loopback addresses that are
# network-namespace local.
# In other words: localhost in the container is not localhost on the host.
#
# We want a consistent name that works from both ends, so we tell containerd to
# alias localhost:${reg_port} to the registry container when pulling images
# Internal container port: registry:2 listens on 5000 by default. reg_port is the host-side
# mapping; containerd inside the kind node reaches the registry container by its docker
# network name on the internal port.
reg_internal_port="${REG_INTERNAL_PORT:-5000}"
REGISTRY_DIR="/etc/containerd/certs.d/localhost:${reg_port}"
for node in $(kind get nodes --name "${KIND_CLUSTER_NAME}"); do
  "${CONTAINER_RUNTIME}" exec "${node}" mkdir -p "${REGISTRY_DIR}"
  cat <<EOF | "${CONTAINER_RUNTIME}" exec -i "${node}" cp /dev/stdin "${REGISTRY_DIR}/hosts.toml"
[host."${reg_scheme}://${reg_name}:${reg_internal_port}"]
  skip_verify = true
EOF
done

# 4. Connect the registry to the cluster network if not already connected
# This allows kind to bootstrap the network but ensures they're on the same network
if [ "$("${CONTAINER_RUNTIME}" inspect -f='{{json .NetworkSettings.Networks.kind}}' "${reg_name}")" = 'null' ]; then
  "${CONTAINER_RUNTIME}" network connect "kind" "${reg_name}"
fi

# 5. Document the local registry
# https://github.com/kubernetes/enhancements/tree/master/keps/sig-cluster-lifecycle/generic/1755-communicating-a-local-registry
cat <<EOF | kubectl --context "kind-${KIND_CLUSTER_NAME}" apply -f -
apiVersion: v1
kind: ConfigMap
metadata:
  name: local-registry-hosting
  namespace: kube-public
data:
  localRegistryHosting.v1: |
    host: "localhost:${reg_port}"
    hostFromClusterNetwork: "${reg_name}:${reg_internal_port}"
    help: "https://kind.sigs.k8s.io/docs/user/local-registry/"
EOF
