#!/usr/bin/env bash

# Install CSI volume snapshots into an existing Kind cluster: the external
# snapshotter (its CRDs and the snapshot controller) and the CSI hostpath driver
# with its VolumeSnapshotClass, plus a StorageClass for it. The workspace
# fixture of the e2e suite provisions, snapshots and restores its volumes here.
set -o errexit -o nounset -o pipefail

SNAPSHOTTER_VERSION=${SNAPSHOTTER_VERSION:-v8.6.0}
HOSTPATH_VERSION=${HOSTPATH_VERSION:-v1.18.0}
KIND_CLUSTER_NAME=${KIND_CLUSTER_NAME:-kagent}
export KUBECTL_CONTEXT="kind-${KIND_CLUSTER_NAME}"
kubectl() { command kubectl --context "$KUBECTL_CONTEXT" "$@"; }
export -f kubectl

checkout=$(mktemp -d "${TMPDIR:-/var/tmp}/kagent-csi.XXXXXX")
trap 'rm -rf "$checkout"' EXIT
for repository in external-snapshotter:"$SNAPSHOTTER_VERSION" csi-driver-host-path:"$HOSTPATH_VERSION"; do
  git -c advice.detachedHead=false clone --quiet --depth 1 --branch "${repository#*:}" --single-branch \
    "https://github.com/kubernetes-csi/${repository%%:*}.git" "$checkout/${repository%%:*}"
done

kubectl apply --kustomize "$checkout/external-snapshotter/client/config/crd"
kubectl apply --kustomize "$checkout/external-snapshotter/deploy/kubernetes/snapshot-controller"
kubectl rollout status deployment/snapshot-controller -n kube-system --timeout=180s

# The driver's own installer applies its sidecars' RBAC, the plugin and the
# csi-hostpath-snapclass VolumeSnapshotClass, and waits for the plugin.
"$checkout/csi-driver-host-path/deploy/kubernetes-latest/deploy.sh"
kubectl apply -f "$checkout/csi-driver-host-path/examples/csi-storageclass.yaml"
kubectl wait --for=condition=Ready pod -l app.kubernetes.io/name=csi-hostpathplugin --all-namespaces --timeout=180s
