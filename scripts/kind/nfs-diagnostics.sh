#!/usr/bin/env bash

# Diagnostics of the in-cluster NFS server behind csi-nfs-sc (setup-csi-nfs.sh)
# for the e2e artifact. The server runs the host kernel's nfsd in the pod's
# network namespace, so its state is read inside the pod and the kernel's
# messages and pressure on the host.
#
#   scripts/kind/nfs-diagnostics.sh watch <dir>  # every NFS_WATCH_INTERVAL
#       seconds (default 15) until killed, append to <dir>/watch.log: the host's
#       CPU, memory and I/O pressure and free memory, the nfsd threads and pool
#       statistics, and whether nfsd answers an NFSv4 NULL call within 5s
#   scripts/kind/nfs-diagnostics.sh dump <dir>   # write the claims and volumes,
#       the server's and the CSI driver's full logs and descriptions, the
#       server's nfsd state and exports, and the host's kernel messages
set -o nounset -o pipefail

KIND_CLUSTER_NAME=${KIND_CLUSTER_NAME:-kagent}
kubectl=(kubectl --context "kind-${KIND_CLUSTER_NAME}")

mode=${1:-}
dir=${2:-}
if [ -z "$dir" ] || { [ "$mode" != watch ] && [ "$mode" != dump ]; }; then
  echo "usage: $0 watch|dump <dir>" >&2
  exit 64
fi
mkdir -p "$dir"

# What nfsd reports inside the server's pod: its thread count, the per-pool
# counters (packets, sockets enqueued, threads woken, threads timed out), the
# RPC counters, and a NULL call to NFSv4 over TCP, which an nfsd thread answers.
# shellcheck disable=SC2016 # expanded in the pod
nfsd_state='
echo "threads: $(cat /proc/fs/nfsd/threads)"
cat /proc/fs/nfsd/pool_stats
grep -E "^(rc|th|io|net|rpc) " /proc/net/rpc/nfsd
start=$(date +%s%N)
if timeout 5 rpcinfo -t 127.0.0.1 nfs 4 >/dev/null 2>&1; then
  echo "nfsv4 NULL call: answered in $(( ($(date +%s%N) - start) / 1000000 ))ms"
else
  echo "nfsv4 NULL call: no answer within 5s"
fi'

host_pressure() {
  for resource in cpu memory io; do
    printf '%s %s\n' "$resource" "$(tr '\n' ' ' < "/proc/pressure/$resource" 2>/dev/null)"
  done
  free -m | sed -n 2p
}

case $mode in
  watch)
    while true; do
      {
        echo "=== $(date -u +%Y-%m-%dT%H:%M:%SZ)"
        host_pressure
        timeout 20 "${kubectl[@]}" exec -n default deployment/nfs-server -- sh -c "$nfsd_state" 2>&1 || echo "nfsd state: not read within 20s"
      } >> "$dir/watch.log"
      sleep "${NFS_WATCH_INTERVAL:-15}"
    done
    ;;
  dump)
    "${kubectl[@]}" get pvc,pv -A -o wide > "$dir/volumes.txt" 2>&1
    "${kubectl[@]}" describe pods -n default -l app=nfs-server > "$dir/nfs-server.describe.txt" 2>&1
    "${kubectl[@]}" describe pods -n kube-system -l 'app in (csi-nfs-controller,csi-nfs-node)' > "$dir/csi-nfs.describe.txt" 2>&1
    "${kubectl[@]}" logs -n default deployment/nfs-server --timestamps --tail=-1 > "$dir/nfs-server.log" 2>&1
    "${kubectl[@]}" logs -n default deployment/nfs-server --timestamps --tail=-1 --previous > "$dir/nfs-server.previous.log" 2>&1
    "${kubectl[@]}" logs -n kube-system -l app=csi-nfs-controller --all-containers --prefix --timestamps --tail=-1 > "$dir/csi-nfs-controller.log" 2>&1
    "${kubectl[@]}" logs -n kube-system -l app=csi-nfs-node --all-containers --prefix --timestamps --tail=-1 > "$dir/csi-nfs-node.log" 2>&1
    {
      timeout 20 "${kubectl[@]}" exec -n default deployment/nfs-server -- sh -c "$nfsd_state"
      # shellcheck disable=SC2016 # expanded in the pod
      timeout 20 "${kubectl[@]}" exec -n default deployment/nfs-server -- sh -c '
        echo "--- exports"; exportfs -v
        echo "--- versions: $(cat /proc/fs/nfsd/versions)"
        echo "--- clients"; for c in /proc/fs/nfsd/clients/*/info; do [ -f "$c" ] && cat "$c"; done
        echo "--- export usage"; df -h /exports; du -sh /exports'
    } > "$dir/nfsd.txt" 2>&1
    {
      host_pressure
      echo "--- kernel messages"
      sudo -n dmesg --ctime || dmesg --ctime
    } > "$dir/host.txt" 2>&1
    echo "NFS diagnostics written to $dir"
    ;;
esac
