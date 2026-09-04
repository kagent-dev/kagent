#!/usr/bin/env bash
# Stand up a kagent dev cluster from nothing, on this machine (arm64).
#
# The steps are the ones .github/workflows/ci.yaml runs to stand up its own cluster,
# plus the two it does not need and a developer does. The order matters: every
# substrate workload mounts secrets that do not exist until the kubectl-ate commands
# have run. README.md beside this file is the reader's version of the same thing.
set -euo pipefail

# The repo this script lives in, so it works from any checkout and any directory.
REPO="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
SUBSTRATE_VERSION=0.0.24
cd "$REPO"

step() { printf '\n\033[1;36m==> %s\033[0m\n' "$1"; }

step "1/10  Kind cluster and local registry on :5001"
make create-kind-cluster

step "2/10  kubectl-ate, the tool that mints the CA and JWT pools"
# This one runs on *this* machine rather than in the cluster, so it follows the host OS.
OS="$(uname -s | tr '[:upper:]' '[:lower:]')"
HOSTARCH="$(uname -m)"; [ "$HOSTARCH" = "x86_64" ] && HOSTARCH=amd64; [ "$HOSTARCH" = "aarch64" ] && HOSTARCH=arm64
if [ ! -x /tmp/kubectl-ate ]; then
  curl -fsSL -o /tmp/kubectl-ate \
    "https://github.com/kagent-dev/substrate/releases/download/v${SUBSTRATE_VERSION}/kubectl-ate-${OS}-${HOSTARCH}"
  chmod +x /tmp/kubectl-ate
fi
ATE=/tmp/kubectl-ate

step "3/10  Substrate CRDs and substrate"
helm upgrade --install substrate-crds \
  "oci://ghcr.io/kagent-dev/substrate/helm/substrate-crds" --version "$SUBSTRATE_VERSION" \
  --namespace ate-system --create-namespace
helm upgrade --install substrate \
  "oci://ghcr.io/kagent-dev/substrate/helm/substrate" --version "$SUBSTRATE_VERSION" \
  --namespace ate-system \
  --set-string 'atelet.extraArgs[0]=--localhost-registry-replacement=kind-registry:5000'

step "4/10  CA and JWT pools"
kubectl create namespace podcertificate-controller-system --dry-run=client -o yaml | kubectl apply -f -
$ATE --context kind-kagent admin make-ca-pool  --ca-id=1 --name=service-dns-ca-pool  --secret-namespace=podcertificate-controller-system
$ATE --context kind-kagent admin make-ca-pool  --ca-id=1 --name=pod-identity-ca-pool --secret-namespace=podcertificate-controller-system
$ATE --context kind-kagent admin make-jwt-pool --key-id=1 --name=actor-id-jwt-pool   --secret-namespace=ate-system
$ATE --context kind-kagent admin make-ca-pool  --ca-id=1 --name=actor-id-ca-pool     --secret-namespace=ate-system

# kubectl-ate prints "Successfully created" and exits 0 slightly BEFORE the secret is
# readable, so wait on the secret rather than trusting the exit code. Found the hard
# way: the next step mounts it and fails on a cluster that has just been told it exists.
for i in $(seq 1 60); do
  kubectl get secret actor-id-ca-pool -n ate-system >/dev/null 2>&1 && break
  sleep 2
done

step "5/10  Actor identity CA cert and the API authentication ConfigMap"
actor_id_ca_root="$(kubectl get secret actor-id-ca-pool -n ate-system -o jsonpath='{.data.pool}' \
  | base64 --decode | jq -r '.CAs[0].RootCertificateDER' | base64 --decode \
  | openssl x509 -inform der -outform pem)"
kubectl create secret generic actor-id-ca-certs -n ate-system \
  --from-literal=ca.crt="${actor_id_ca_root}" --dry-run=client -o yaml | kubectl apply -f -
kubectl create configmap ate-api-authentication -n ate-system \
  --from-literal=authentication.yaml=$'actorIdentityJWTProvider: kubernetes\njwtProviders:\n- name: kubernetes\n  issuer: https://kubernetes.default.svc\n  audiences: [api.ate-system.svc]\n  certificateAuthorityFile: /var/run/secrets/kubernetes.io/serviceaccount/ca.crt\n  discoveryTokenFile: /var/run/secrets/kubernetes.io/serviceaccount/token\n' \
  --dry-run=client -o yaml | kubectl apply -f -

helm upgrade substrate "oci://ghcr.io/kagent-dev/substrate/helm/substrate" \
  --version "$SUBSTRATE_VERSION" --namespace ate-system --reuse-values --wait --timeout 5m

step "6/10  kagent"
make helm-install KAGENT_HELM_EXTRA_ARGS="\
  --set controller.substrate.enabled=true \
  --set controller.substrate.ateApiEndpoint=dns:///api.ate-system.svc:443 \
  --set controller.substrate.atenetRouterURL=http://atenet-router.ate-system.svc:80 \
  --set controller.substrate.defaultWorkerPool.name=kagent-default \
  --set substrateWorkerPool.create=true \
  --set substrateWorkerPool.replicas=8 \
  --set-string substrateWorkerPool.workerImage=ghcr.io/kagent-dev/substrate/ateom-gvisor:v${SUBSTRATE_VERSION}"

step "7/10  The controller and the UI, both built from this checkout"
# The chart installs published images, so without this the cluster would run somebody
# else's build and none of the local changes would be on it. Both are replaced.
#
# Built for this machine's own architecture: the images run on the Kind node, which is
# a container on this host, so a cross-built one would not start.
ARCH="$(uname -m)"; [ "$ARCH" = "x86_64" ] && ARCH=amd64; [ "$ARCH" = "aarch64" ] && ARCH=arm64
docker buildx build --push --platform "linux/${ARCH}" \
  --build-arg BASE_IMAGE_REGISTRY=cgr.dev \
  --build-arg BUILD_PACKAGE=core/cmd/controller/main.go \
  -t localhost:5001/kagent-dev/kagent/controller:dev -f go/Dockerfile ./go
kubectl -n kagent set image deploy/kagent-controller controller=localhost:5001/kagent-dev/kagent/controller:dev

docker buildx build --push --platform "linux/${ARCH}" \
  -t localhost:5001/kagent-dev/kagent/ui:dev -f ui/Dockerfile ./ui
kubectl -n kagent set image deploy/kagent-ui ui=localhost:5001/kagent-dev/kagent/ui:dev

kubectl -n kagent rollout status deploy/kagent-controller --timeout=5m
kubectl -n kagent rollout status deploy/kagent-ui --timeout=5m

step "8/10  The agent runtime image, pinned by digest"
# The Go ADK, which is what the chart names as the image for declarative agents, and
# the one that survives being an actor: an actor starts by restoring the template's
# golden snapshot, and the Python runtime dies on restore with SIGILL where a static
# Go binary comes back. Substrate requires a digest, and only a registry can give one,
# so it is pushed rather than loaded.
docker buildx build --push --platform "linux/${ARCH}" \
  --build-arg BASE_IMAGE_REGISTRY=cgr.dev \
  --build-arg BUILD_PACKAGE=adk/cmd/main.go \
  -t localhost:5001/kagent-dev/kagent/golang-adk:dev -f go/Dockerfile ./go
HARNESS_DIGEST="$(docker buildx imagetools inspect localhost:5001/kagent-dev/kagent/golang-adk:dev \
  | awk '/^Digest:/{print $2; exit}')"

step "9/10  A harness and an agent template, so the app has an agent in it"
# An agent is a Harness x AgentTemplate pair, so both are needed before anything is
# listed. The harness admits templates by label, and the template carries the label it
# admits -- a template no harness admits is created successfully and then does nothing,
# which is the single most confusing state to arrive in.
kubectl apply -f - <<EOF
apiVersion: kagent.dev/v1alpha3
kind: Harness
metadata:
  name: kagent
  namespace: kagent
spec:
  kagent: {}
  workload:
    image: localhost:5001/kagent-dev/kagent/golang-adk@${HARNESS_DIGEST}
  substrate:
    workerPoolRef:
      name: kagent-default
    snapshotPolicy:
      location: s3://ate-snapshots/kagent
  allowedAgentTemplates:
    selector:
      matchLabels:
        kagent.dev/harness: kagent
---
apiVersion: kagent.dev/v1alpha3
kind: AgentTemplate
metadata:
  name: assistant
  namespace: kagent
  labels:
    kagent.dev/harness: kagent
spec:
  modelConfig:
    name: default-model-config
  description: A general-purpose assistant.
  systemPrompt: You are a helpful assistant running on kagent.
EOF

# Ready means Substrate has booted the template's golden actor and snapshotted it, which
# takes a minute or so. Waited for here rather than left to the reader, because an agent
# that is not ready yet is listed but refuses conversations, and nothing on the page
# explains that it is a matter of waiting.
printf 'waiting for the agent to become ready'
for _ in $(seq 1 40); do
  ready="$(kubectl get agenttemplate -n kagent assistant \
    -o jsonpath='{.status.harnesses[0].conditions[?(@.type=="Ready")].status}' 2>/dev/null || true)"
  [ "$ready" = "True" ] && break
  printf '.'; sleep 15
done
echo
kubectl get agenttemplate -n kagent assistant \
  -o jsonpath='agent assistant x kagent: Ready={.status.harnesses[0].conditions[?(@.type=="Ready")].status}{"\n"}'

step "10/10  Done"
kubectl get pods -n kagent

# Both forwards a developer needs, held open together.
#
# 8080 is the UI the cluster is running -- the image built above, served by its nginx,
# not a dev server. 8083 is the controller, which `yarn dev` proxies to by default.
#
# The second one is here so that default is true. With only the UI forwarded, running
# the dev server needed KAGENT_DEV_CONTROLLER_URL pointed at 8080 in `ui/.env`, and
# without that line every read failed with `ECONNREFUSED 127.0.0.1:8083` on a page that
# otherwise loaded -- which reads as a broken backend rather than a missing forward. A
# second `kubectl` is cheaper than a setting every reader has to be told about.
#
# Backgrounded and waited on rather than `exec`ed, because two of them cannot both be
# the foreground process. The trap is what makes Ctrl-C take both down: without it the
# script would exit and leave orphaned forwards holding the ports, and the next run
# would fail on an address already in use.
kubectl -n kagent port-forward svc/kagent-ui 8080:8080 &
UI_FORWARD=$!
kubectl -n kagent port-forward svc/kagent-controller 8083:8083 &
CONTROLLER_FORWARD=$!
trap 'kill "$UI_FORWARD" "$CONTROLLER_FORWARD" 2>/dev/null || true' EXIT INT TERM

printf '\n\033[1;32m==> The UI is at http://localhost:8080\033[0m\n'
printf '    The controller is on :8083, which `cd ui && yarn dev` uses by default.\n'
printf '    (both port-forwards running in this shell; Ctrl-C to stop)\n\n'

# Held until either one dies, because a forward that has gone means whatever depended on
# it is now failing and saying so beats leaving one working and one silently absent.
#
# Polled rather than `wait -n`, which is bash 4.3 and this is a script for a Mac: the
# system bash here is 3.2, where `wait -n` is a syntax error and `wait` alone would sit
# on a dead forward until the other one went too.
while kill -0 "$UI_FORWARD" 2>/dev/null && kill -0 "$CONTROLLER_FORWARD" 2>/dev/null; do
  sleep 1
done
