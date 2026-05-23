# OpenClaw on Agent Substrate

## 1. Install Substrate on your Kind cluster

Uses cluster `kind` (`KIND_CLUSTER_NAME=kind`; or set `KUBECONFIG` / context accordingly).

```bash
cd substrate

./hack/create-kind-cluster.sh
./hack/install-ate-kind.sh --deploy-ate-system
```

`--deploy-ate-system` installs the **control plane only** (ate-api, ate-controller, atelet, atenet, …). Your registry catalog will show `ateapi-*`, `atelet-*`, etc., but **not** ateom until you build it.

Build and push **ateom-gvisor** (required for kagent `workerPool.ateomImage`):

```bash
# build the ateom-gvisor image from the substrate folder
export KO_DOCKER_REPO=localhost:5001
export KO_DEFAULTPLATFORMS=linux/$(go env GOARCH)
./hack/ko.sh build -B ./cmd/servers/ateom-gvisor
```

## 2. Load nemoclaw image

The image is a multi-arch manifest list. On Apple Silicon, `kind load docker-image` often fails with `content digest ... not found` because Docker only has the local arch locally while kind imports with `--all-platforms`. Use `docker save` + `ctr import` instead (match `--name` to your cluster, e.g. `agent` for context `kind-agent`):

```bash
docker pull --platform linux/arm64 ghcr.io/kagent-dev/nemoclaw/sandbox-base:2026.5.4
docker save ghcr.io/kagent-dev/nemoclaw/sandbox-base:2026.5.4 | \
  docker exec -i kind-control-plane ctr --namespace=k8s.io images import -
```

On amd64 hosts, use `--platform linux/amd64` in the pull step.

## kagent AgentHarness with substrate runtime

kagent **auto-provisions** a per-harness `ActorTemplate` (and optionally a `WorkerPool`).

Install kagent (Substrate must already be running in the cluster):

```bash
export KIND_CLUSTER_NAME=kind
make helm-install KAGENT_HELM_EXTRA_ARGS="--set controller.substrate.enabled=true"
```

Create a harness with only what you must choose:

- **`snapshotsConfig.location`** — GCS `gs://` prefix (Substrate snapshots are GCS-only today)
- **Worker pool** — reference an existing pool (`workerPoolRef`) **or** let kagent create one (`workerPool` + **`ateomImage`**)
- **`workerPool.ateomImage`** — (`localhost:5001/ateom-gvisor:latest`)

```yaml
apiVersion: kagent.dev/v1alpha2
kind: AgentHarness
metadata:
  name: peterj-claw
  namespace: kagent
spec:
  runtime: substrate
  backend: openclaw
  description: OpenClaw on Agent Substrate
  modelConfigRef: default-model-config
  substrate:
    snapshotsConfig:
      location: gs://ate-snapshots/kagent/kagent/my-claw/
    workerPool:
      replicas: 1
      ateomImage: localhost:5001/ateom-gvisor:latest
    # Optional: adopt existing resources instead of auto-create
    # workerPoolRef:
    #   name: my-pool
    #   namespace: ate-system
```

Port-forward the UI (`kubectl port-forward -n kagent svc/kagent-ui 8001:8080`) and navigate to the deployed agent harness. Use `test-token` as a gateway token to OpenClaw.
