#!/usr/bin/env bash

# Install Cloud Hypervisor assets and SandboxConfig into an existing Kind cluster
# after the Substrate Helm release has been installed.
set -o errexit -o nounset -o pipefail

: "${SUBSTRATE_VERSION:?Set SUBSTRATE_VERSION to the installed Helm release version}"
cluster_name=${KIND_CLUSTER_NAME:-kagent}
asset_cache="$(git rev-parse --show-toplevel)/.cache/substrate/microvm-assets"

# Substrate owns the asset versions, checksums, staging, and SandboxConfig. Use
# its installer from the same release as the chart and worker image.
substrate_dir=$(mktemp -d "${TMPDIR:-/var/tmp}/kagent-substrate.XXXXXX")
trap 'rm -rf "$substrate_dir"' EXIT
git clone --depth 1 --branch "v${SUBSTRATE_VERSION}" --single-branch \
  https://github.com/kagent-dev/substrate.git "$substrate_dir"
# Retain the installer's outputs across temporary source checkouts. ate-setup
# owns the asset layout and verifies its cached versions and checksums.
mkdir -p "$asset_cache"
ln -s "$asset_cache" "$substrate_dir/bin"

go -C "$substrate_dir" run -mod=readonly ./cmd/ate-setup \
  --kind --kind-cluster-name "$cluster_name" --context "kind-${cluster_name}" \
  deploy sandboxconfig --sandbox-classes=microvm
