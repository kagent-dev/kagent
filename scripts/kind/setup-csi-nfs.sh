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

# The NFS server's pod is available before its nfsd answers (it has no
# readiness probe, and nfsd waits out client tracking and a 10-second grace
# period after "NFS started"), so the setup ends only once a claim of
# csi-nfs-sc is provisioned, mounted, written and read back, and deleted.
# The probe runs the server's own image, which the node already holds.
NFS_READY_TIMEOUT=${NFS_READY_TIMEOUT:-300s}
nfs_image=$(kubectl get deployment nfs-server -n default -o jsonpath='{.spec.template.spec.containers[0].image}')
kubectl apply -f - <<EOF
apiVersion: v1
kind: PersistentVolumeClaim
metadata:
  name: nfs-readiness
  namespace: default
spec:
  accessModes: [ReadWriteMany]
  storageClassName: csi-nfs-sc
  resources:
    requests:
      storage: 1Mi
---
apiVersion: batch/v1
kind: Job
metadata:
  name: nfs-readiness
  namespace: default
spec:
  backoffLimit: 10
  template:
    spec:
      restartPolicy: Never
      containers:
        - name: probe
          image: ${nfs_image}
          imagePullPolicy: IfNotPresent
          command: [sh, -c, 'echo ready > /data/probe && sync && test "\$(cat /data/probe)" = ready']
          volumeMounts:
            - name: data
              mountPath: /data
      volumes:
        - name: data
          persistentVolumeClaim:
            claimName: nfs-readiness
EOF
if ! kubectl wait --for=condition=Complete job/nfs-readiness -n default --timeout="$NFS_READY_TIMEOUT"; then
  echo "ERROR: no claim of csi-nfs-sc was mounted and written within ${NFS_READY_TIMEOUT}." >&2
  kubectl describe job,pvc -n default nfs-readiness >&2 || true
  kubectl describe pods -n default -l job-name=nfs-readiness >&2 || true
  kubectl logs -n default deployment/nfs-server >&2 || true
  exit 1
fi
kubectl delete job/nfs-readiness pvc/nfs-readiness -n default --cascade=foreground --wait --timeout=120s
echo "NFS server answers: a csi-nfs-sc claim was mounted, written and deleted."
