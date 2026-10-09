#!/usr/bin/env bash
# Stand up the CI e2e cluster: Kind, Substrate, and kagent built from this checkout.
#
# Independent steps run concurrently; each step starts as soon as the steps it
# depends on finish. Per-step durations are written to $TIMINGS_FILE.
#
# Dependency graph:
#   start-registry runs first; everything else may push to or reuse the registry.
#   create-kind-cluster --> deploy-metallb
#   create-kind-cluster --> preload-images
#   create-kind-cluster --> install-substrate
#   download-kubectl-ate --> install-substrate
#   install-substrate --> install-microvm (microvm only)
#   build-images, package-helm-charts, deploy-metallb, install-substrate, install-microvm --> install-kagent
#   warm-go-test runs alongside everything; it only fills the Go build cache.

set -o errexit
set -o pipefail
set -o nounset

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "$REPO_ROOT"

: "${SUBSTRATE_VERSION:?}"
: "${VERSION:?}"
KIND_SANDBOX_CLASS="${KIND_SANDBOX_CLASS:-gvisor}"
KIND_CLUSTER_NAME="${KIND_CLUSTER_NAME:-kagent}"
REG_NAME="${REG_NAME:-kind-registry}"
REG_PORT="${REG_PORT:-5001}"
WORK_DIR="$(mktemp -d "${RUNNER_TEMP:-/tmp}/kagent-e2e-setup.XXXXXX")"
TIMINGS_FILE="${TIMINGS_FILE:-${WORK_DIR}/timings.log}"
KUBECTL_ATE="${WORK_DIR}/kubectl-ate"
# The kind scripts prefer podman when installed (as on CI runners); the
# registry, buildx, and Kind must all share docker.
CONTAINER_RUNTIME=docker
export SUBSTRATE_VERSION VERSION KIND_SANDBOX_CLASS KIND_CLUSTER_NAME CONTAINER_RUNTIME
: >"${TIMINGS_FILE}"

run_step() {
  local name="$1"
  shift
  local start rc
  start="$(date +%s)"
  echo "==> Step started: ${name}" >&2
  # Not `if "$@"`: bash ignores errexit inside a condition, so a failing command
  # mid-step would not stop the step.
  set +o errexit
  (set -o errexit; "$@") 2>&1 | sed -u "s/^/[${name}] /"
  rc="${PIPESTATUS[0]}"
  set -o errexit
  printf '%s: %ss\n' "${name}" "$(($(date +%s) - start))" >>"${TIMINGS_FILE}"
  if [[ "${rc}" -ne 0 ]]; then
    echo "==> Step failed: ${name} (exit ${rc})" >&2
    return "${rc}"
  fi
  touch "${WORK_DIR}/${name}.ok"
  echo "==> Step completed: ${name} ($(($(date +%s) - start))s)" >&2
}

# step NAME DEPS FUNC [ARGS...] runs FUNC in the background once every step in
# the space-separated DEPS has succeeded. If a dependency fails, NAME is skipped.
step() {
  local name="$1" deps="$2"
  shift 2
  (
    for dep in ${deps}; do
      local pid_var="PID_${dep//-/_}"
      # `wait` cannot be used on sibling pids; GNU tail can block on any pid.
      tail -s 0.1 --pid="${!pid_var}" -f /dev/null
      if [[ ! -f "${WORK_DIR}/${dep}.ok" ]]; then
        echo "==> Step skipped: ${name} (${dep} failed)" >&2
        exit 1
      fi
    done
    run_step "${name}" "$@"
  ) &
  printf -v "PID_${name//-/_}" '%s' "$!"
}

step_start_registry() {
  # Same container setup-kind.sh would create; starting it first lets image
  # builds push while the cluster is still coming up.
  if [[ "$(docker inspect -f '{{.State.Running}}' "${REG_NAME}" 2>/dev/null || true)" != 'true' ]]; then
    docker run -d --restart=always -p "127.0.0.1:${REG_PORT}:5000" --network bridge --name "${REG_NAME}" registry:2
  fi
}

step_create_kind_cluster() {
  KIND_WAIT=0s bash scripts/kind/setup-kind.sh
}

step_deploy_metallb() {
  bash scripts/kind/setup-metallb.sh
}

step_preload_images() {
  # Best effort: images needed once kagent is installed. Pods pull them anyway.
  local node="${KIND_CLUSTER_NAME}-control-plane"
  docker exec "${node}" crictl pull "ghcr.io/kagent-dev/substrate/ateom-${KIND_SANDBOX_CLASS}:v${SUBSTRATE_VERSION}" &
  docker exec "${node}" crictl pull docker.io/pgvector/pgvector:pg18-trixie &
  wait
}

step_download_kubectl_ate() {
  curl -fsSL -o "${KUBECTL_ATE}" "https://github.com/kagent-dev/substrate/releases/download/v${SUBSTRATE_VERSION}/kubectl-ate-linux-amd64"
  chmod +x "${KUBECTL_ATE}"
}

step_install_substrate() {
  local chart=oci://ghcr.io/kagent-dev/substrate/helm
  helm upgrade --install substrate-crds "${chart}/substrate-crds" --version "${SUBSTRATE_VERSION}" --namespace ate-system --create-namespace
  helm upgrade --install substrate "${chart}/substrate" --version "${SUBSTRATE_VERSION}" --namespace ate-system \
    --set-string 'atelet.extraArgs[0]=--localhost-registry-replacement=kind-registry:5000' \
    --set-string 'ateApi.extraArgs[0]=--template-resync-interval=250ms' \
    --set 'credentialProvider.namespacePolicies[0].atespace=kagent' \
    --set 'credentialProvider.namespacePolicies[0].allowedNamespaces[0]=kagent'
  local ate=("${KUBECTL_ATE}" --context "kind-${KIND_CLUSTER_NAME}" admin)
  "${ate[@]}" make-ca-pool --ca-id=1 --name=service-dns-ca-pool --secret-namespace=podcertificate-controller-system
  "${ate[@]}" make-ca-pool --ca-id=1 --name=pod-identity-ca-pool --secret-namespace=podcertificate-controller-system
  "${ate[@]}" make-jwt-pool --key-id=1 --name=actor-id-jwt-pool --secret-namespace=ate-system
  "${ate[@]}" make-ca-pool --ca-id=1 --name=actor-id-ca-pool --secret-namespace=ate-system
  "${ate[@]}" make-ca-pool --ca-id=1 --name=egress-mitm-ca-pool --secret-namespace=ate-system --key-type=ECDSAP256
  # kubectl-ate exits slightly before its secret is readable.
  for _ in $(seq 1 60); do
    kubectl get secret actor-id-ca-pool -n ate-system >/dev/null 2>&1 && break
    sleep 1
  done
  local actor_id_ca_root
  actor_id_ca_root="$(kubectl get secret actor-id-ca-pool -n ate-system -o jsonpath='{.data.pool}' | base64 --decode | jq -r '.CAs[0].RootCertificateDER' | base64 --decode | openssl x509 -inform der -outform pem)"
  kubectl create secret generic actor-id-ca-certs -n ate-system --from-literal=ca.crt="${actor_id_ca_root}"
  kubectl create configmap ate-api-authentication -n ate-system --from-literal=authentication.yaml=$'actorIdentityJWTProvider: kubernetes\njwtProviders:\n- name: kubernetes\n  issuer: https://kubernetes.default.svc\n  audiences: [api.ate-system.svc]\n  certificateAuthorityFile: /var/run/secrets/kubernetes.io/serviceaccount/ca.crt\n  discoveryTokenFile: /var/run/secrets/kubernetes.io/serviceaccount/token\n'
  helm upgrade substrate "${chart}/substrate" --version "${SUBSTRATE_VERSION}" --namespace ate-system --reuse-values --wait --timeout 5m
}

step_install_microvm() {
  bash scripts/kind/setup-microvm.sh
}

step_build_images() {
  # Reuse Blacksmith's persistent layers and Go cache mounts. The Makefile
  # otherwise selects a fresh local builder, discarding that cache.
  # Read all output: exiting awk early can kill docker with SIGPIPE, which
  # pipefail turns into a silent exit 255.
  BUILDX_BUILDER_NAME=$(docker buildx inspect | awk '$1 == "Name:" && !found { print $2; found = 1 }')
  test -n "${BUILDX_BUILDER_NAME}"
  export BUILDX_BUILDER_NAME
  make buildx-create
  printf '%s\n' controller golang-adk claude-harness codex-harness byo-a2a sandbox-guest | xargs -P4 -n1 bash -c '
    image="$1"
    DOCKER_BUILD_ARGS="--platform=linux/amd64 --push" \
      make GIT_COMMIT=e2e BUILD_DATE=1970-01-01 "build-${image}"
  ' _
}

step_package_helm_charts() {
  make helm-version
}

step_warm_go_test() {
  make -C go core/bin/kagent-local
  (cd go && go test -c -o /dev/null ./core/test/e2e)
}

step_install_kagent() {
  # Resolve the guest on the runner: localhost:5001 is not reachable
  # from the controller pod. Substrate rewrites registry pulls on workers.
  local guest_digest
  guest_digest=$(docker buildx imagetools inspect "localhost:${REG_PORT}/kagent-dev/kagent/sandbox-guest:${VERSION}" | awk '$1 == "Digest:" { print $2 }')
  test -n "${guest_digest}"
  export KAGENT_HELM_EXTRA_ARGS="${KAGENT_HELM_EXTRA_ARGS:-} --set-string controller.sandbox.guestImage.digest=${guest_digest}"
  # Charts were already packaged by package-helm-charts.
  make -o helm-version helm-install-provider
  kubectl rollout status deployment/kagent-controller -n kagent --timeout=120s
  kubectl wait --for=condition=Ready pod -l app.kubernetes.io/component=controller -n kagent --timeout=120s
}

main() {
  echo "Timings will be written to: ${TIMINGS_FILE}"

  run_step start-registry step_start_registry

  step create-kind-cluster "" step_create_kind_cluster
  step build-images "" step_build_images
  step package-helm-charts "" step_package_helm_charts
  step download-kubectl-ate "" step_download_kubectl_ate
  step warm-go-test "" step_warm_go_test

  step deploy-metallb "create-kind-cluster" step_deploy_metallb
  step preload-images "create-kind-cluster" step_preload_images
  step install-substrate "create-kind-cluster download-kubectl-ate" step_install_substrate

  local kagent_deps="build-images package-helm-charts deploy-metallb install-substrate"
  if [[ "${KIND_SANDBOX_CLASS}" == microvm ]]; then
    step install-microvm "install-substrate" step_install_microvm
    kagent_deps+=" install-microvm"
  fi
  step install-kagent "${kagent_deps}" step_install_kagent

  # Wait on each job, not a bare `wait`, so any failure fails the script.
  local rc=0
  for pid in $(jobs -p); do
    wait "${pid}" || rc=1
  done
  echo "Step timings:"
  cat "${TIMINGS_FILE}"
  return "${rc}"
}

main "$@"
