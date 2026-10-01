#!/usr/bin/env bash

# Install Cloud Hypervisor assets and SandboxConfig into an existing Kind cluster
# after the Substrate Helm release has been installed.
set -o errexit -o nounset -o pipefail

: "${SUBSTRATE_VERSION:?Set SUBSTRATE_VERSION to the installed Helm release version}"
export KIND_CLUSTER_NAME=${KIND_CLUSTER_NAME:-kagent}
export KUBECTL_CONTEXT="kind-${KIND_CLUSTER_NAME}"
export ATE_INSTALL_KIND=true
ARCH=$(go env GOARCH)
OUT="$(git rev-parse --show-toplevel)/.cache/substrate/microvm-assets/${ARCH}"
export ARCH OUT

# Substrate owns the asset versions, checksums, staging, and SandboxConfig. Use
# its installer from the same release as the chart and worker image.
substrate_dir=$(mktemp -d "${TMPDIR:-/var/tmp}/kagent-substrate.XXXXXX")
trap 'rm -rf "$substrate_dir"' EXIT
git clone --depth 1 --branch "v${SUBSTRATE_VERSION}" --single-branch \
  https://github.com/kagent-dev/substrate.git "$substrate_dir"
cd "$substrate_dir"

# The released tool helper uses `go tool -n`, which can return a removed
# temporary binary on a cold cache. Use the Kind already installed on PATH.
# Invoke it by name so version-manager shims resolve the correct command.
cat > hack/kind.sh <<'EOF'
#!/usr/bin/env bash
exec kind "$@"
EOF
hack/install-microvm-deps.sh --install
