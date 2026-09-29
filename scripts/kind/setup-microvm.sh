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

# v0.3.0-alpha1 uses `go tool -n` to locate Kind, returning a nonexistent path
# on a cold cache. Reuse the installed binary until the upstream wrapper is fixed.
# Invoke it by name so version-manager shims still see the correct command name.
cat > hack/kind.sh <<'EOF'
#!/usr/bin/env bash
exec kind "$@"
EOF
hack/install-microvm-deps.sh --install
