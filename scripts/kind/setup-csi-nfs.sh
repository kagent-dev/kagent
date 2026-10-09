#!/usr/bin/env bash

# Install read-write-many storage for Agent Substrate's existing volumes into an
# existing Kind cluster that runs Substrate: an in-cluster NFS server, the NFS
# CSI driver nfs.csi.k8s.io with its node plugin propagating mounts into
# Substrate's /var/lib/ate, the StorageClass csi-nfs-sc, and the driver's
# CSIDriverConfig. It runs the pinned Substrate release's own Kind setup, so the
# driver is wired the way that Substrate expects. The workspace fixture of the
# e2e suite claims its shared volume from csi-nfs-sc. The host kernel must
# provide nfsd and nfs (on a GitHub runner: sudo modprobe nfs nfsd).
set -o errexit -o nounset -o pipefail

SUBSTRATE_VERSION=${SUBSTRATE_VERSION:-$(make -s substrate-pin | sed -n 's/^SUBSTRATE_VERSION=//p')}
SUBSTRATE_GIT=${SUBSTRATE_GIT:-https://github.com/giantswarm/substrate.git}
KIND_CLUSTER_NAME=${KIND_CLUSTER_NAME:-kagent}
export KUBECTL_CONTEXT="kind-${KIND_CLUSTER_NAME}"
kubectl() { command kubectl --context "$KUBECTL_CONTEXT" "$@"; }
export -f kubectl

checkout=$(mktemp -d "${TMPDIR:-/var/tmp}/kagent-nfs.XXXXXX")
trap 'rm -rf "$checkout"' EXIT
git -c advice.detachedHead=false clone --quiet --depth 1 --branch "v${SUBSTRATE_VERSION}" --single-branch \
  "$SUBSTRATE_GIT" "$checkout/substrate"
(cd "$checkout/substrate" && bash hack/setup-csi-nfs-kind.sh)
