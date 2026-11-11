#!/bin/bash
# The cluster was started a moment ago. Wait for its pods to settle (all ready, no restart for 30 seconds)
# before the snapshot is taken, and for the registry the build pushes to.
set -eu
kubectl wait --for=condition=Ready node --all --timeout=180s
curl -fsS --max-time 5 http://localhost:5001/v2/ >/dev/null
last=-1
stable=0
for _ in $(seq 1 90); do
  state=$(kubectl get pods -A --field-selector=status.phase!=Succeeded --no-headers 2>/dev/null |
    awk '{split($3, r, "/"); if (r[1] != r[2] || $4 != "Running") bad++; restarts += $5; n++} END {print n + 0, bad + 0, restarts + 0}') || state="0 1 0"
  set -- $state
  if [ "$1" -gt 0 ] && [ "$2" -eq 0 ] && [ "$3" -eq "$last" ]; then
    stable=$((stable + 1))
  else
    stable=0
  fi
  last=$3
  if [ "$stable" -ge 6 ]; then
    exit 0
  fi
  sleep 5
done
echo "the pods did not settle" >&2
kubectl get pods -A >&2 || true
exit 1
