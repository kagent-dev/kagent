#!/usr/bin/env bash

set -o errexit
set -o pipefail
set -o nounset

METALLB_VERSION=${METALLB_VERSION:-v0.15.3}
KIND_CLUSTER_NAME=${KIND_CLUSTER_NAME:-kagent}

kubectl --context "kind-${KIND_CLUSTER_NAME}" apply -f https://raw.githubusercontent.com/metallb/metallb/${METALLB_VERSION}/config/manifests/metallb-native.yaml

# Wait for MetalLB to become available.
kubectl --context "kind-${KIND_CLUSTER_NAME}" rollout status -n metallb-system deployment/controller --timeout 5m
kubectl --context "kind-${KIND_CLUSTER_NAME}" rollout status -n metallb-system daemonset/speaker --timeout 5m
kubectl --context "kind-${KIND_CLUSTER_NAME}" wait -n metallb-system  pod -l app=metallb --for=condition=Ready --timeout=10s

SUBNET=$(docker network inspect kind | jq -r '.[].IPAM.Config[].Subnet | select(contains(":") | not)' | cut -d '.' -f1,2)
MIN=${SUBNET}.255.0
MAX=${SUBNET}.255.231

# Note: each line below must begin with one tab character; this is to get EOF working within
# an if block. The `-` in the `<<-EOF`` strips out the leading tab from each line, see
# https://tldp.org/LDP/abs/html/here-docs.html
kubectl --context "kind-${KIND_CLUSTER_NAME}" apply -f - <<-EOF
apiVersion: metallb.io/v1beta1
kind: IPAddressPool
metadata:
  name: address-pool
  namespace: metallb-system
spec:
  addresses:
    - ${MIN}-${MAX}
---
apiVersion: metallb.io/v1beta1
kind: L2Advertisement
metadata:
  name: advertisement
  namespace: metallb-system
spec:
  ipAddressPools:
    - address-pool
EOF
