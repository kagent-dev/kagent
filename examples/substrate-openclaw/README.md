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
make helm-install KAGENT_HELM_EXTRA_ARGS="--set controller.substrate.enabled=true --set controller.substrate.ateomImage=localhost:5001/ateom-gvisor:latest"
```

The generated `ActorTemplate` uses `controller.substrate.pauseImage`, `controller.substrate.runscAMD64URL`, `controller.substrate.runscAMD64SHA256`, `controller.substrate.runscARM64URL`, and `controller.substrate.runscARM64SHA256` from the Helm values Override them with `--set` or a values file when you need to pin a different gVisor build.

Create a harness. If `snapshotsConfig` is omitted, kagent defaults it to `gs://ate-snapshots/<namespace>/<agentharnessname>`. If Helm sets `controller.substrate.ateomImage`, the per-harness `workerPool.ateomImage` can be omitted unless you want to override it.

- **Worker pool** — reference an existing pool (`workerPoolRef`) **or** let kagent create one (`workerPool`)
- **`workerPool.ateomImage`** — optional override for the Helm/controller default (`localhost:5001/ateom-gvisor:latest`)
- **Gateway token** — required per harness with either `gatewayToken` or `gatewayTokenSecretRef`

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
    # Optional: defaults to gs://ate-snapshots/kagent/peterj-claw
    # snapshotsConfig:
    #   location: gs://ate-snapshots/kagent/peterj-claw

    # Optional: kagent auto-creates a WorkerPool when workerPoolRef is unset.
    # Replicas default to 1 and ateomImage defaults to controller.substrate.ateomImage.
    workerPool:
      replicas: 2
    #   ateomImage: localhost:5001/ateom-gvisor:latest

    # Required: configure the OpenClaw gateway token for this harness.
    # Use either gatewayToken or gatewayTokenSecretRef. The Secret must contain key "token".
    gatewayToken: test-token
    # gatewayTokenSecretRef:
    #   name: openclaw-gateway-token
    #   namespace: kagent

    # Optional: override the sandbox image used in the ActorTemplate.
    # workloadImage: ghcr.io/kagent-dev/nemoclaw/sandbox-base:2026.5.4

    # Optional: adopt existing resources instead of auto-create
    # workerPoolRef:
    #   name: my-pool
    #   namespace: ate-system
    # actorTemplateRef:
    #   name: my-template
    #   namespace: ate-system
```

When `actorTemplateRef` is not set, kagent creates an `ActorTemplate` that looks roughly like this:

```yaml
apiVersion: ate.dev/v1alpha1
kind: ActorTemplate
metadata:
  name: peterj-claw
  namespace: kagent
  labels:
    app.kubernetes.io/managed-by: kagent
    kagent.dev/agent-harness: peterj-claw
spec:
  pauseImage: gcr.io/gke-release/pause@sha256:bcbd57ba5653580ec647b16d8163cdd1112df3609129b01f912a8032e48265da
  runsc:
    amd64:
      url: gs://gvisor/releases/nightly/2026-05-19/x86_64/runsc
      sha256Hash: a397be1abc2420d26bce6c70e6e2ff96c73aaaab929756c56f5e2089ea842b63
    arm64:
      url: gs://gvisor/releases/nightly/2026-05-19/aarch64/runsc
      sha256Hash: 1ba2366ae2efceba166046f51a4104f9261c9cb72c6db8f5b3fe2dc57dea86b9
  workerPoolRef:
    name: peterj-claw-wp
    namespace: kagent
  snapshotsConfig:
    location: gs://ate-snapshots/kagent/peterj-claw
  containers:
  - name: openclaw
    image: ghcr.io/kagent-dev/nemoclaw/sandbox-base:2026.5.4
    ports:
    - containerPort: 80
    command:
    - /bin/sh
    - -c
    - |
      # Generated by kagent:
      # 1. writes ~/.openclaw/openclaw.json from modelConfigRef/channels/gateway token
      # 2. configures gateway.controlUi.basePath for the kagent proxy path
      # 3. starts `openclaw gateway run --port 80 --allow-unconfigured`
      # 4. waits for the gateway and tails the log
    env:
    - name: HOME
      value: /root
```

The generated `command` contains a base64-encoded `openclaw.json`, so the live object will be more verbose than the abbreviated example above. `pauseImage`, runsc URLs and hashes, and the default workload image come from controller/Helm configuration unless overridden on the `AgentHarness`; the gateway token comes from `spec.substrate.gatewayToken` or `gatewayTokenSecretRef`. kagent also sets `gateway.controlUi.basePath` to `/api/agentharnesses/<namespace>/<name>/gateway` so OpenClaw serves the Control UI under the same path kagent proxies.

Port-forward the UI:

```bash
kubectl port-forward -n kagent svc/kagent-ui 8001:8080
```

Navigate to the deployed agent harness. If the OpenClaw Control UI asks for a gateway connection, use:

- Gateway URL: `http://localhost:8001/api/agentharnesses/kagent/peterj-claw/gateway/`
- Gateway token: `test-token`

The gateway URL must include the trailing slash. The token is the value configured in `spec.substrate.gatewayToken`, or the Secret value referenced by `spec.substrate.gatewayTokenSecretRef`; enter it in the token/credentials field rather than relying on a `token` query parameter.
