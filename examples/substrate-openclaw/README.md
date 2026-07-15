# kagent agents and AgentHarness on Substrate

Follow these instructions to install Substrate on a kind cluster. This feature allows you to run AgentHarness (OpenClaw) and declarative Go SandboxAgents on Agent Substrate.

## 1. Install Substrate on your Kind cluster

This assumes you've configured a kind cluster using `make create-kind-cluster`.

### Image-pull args for a local (kind) registry

atelet is what pulls the ActorTemplate container image for each golden actor. It uses
`go-containerregistry` directly (not containerd), so containerd's registry-mirror
config on the kind node does **not** apply to atelet — you have to tell atelet how to
reach the local kind registry with its own flags:

```yaml
atelet:
  # Skip the GCP application-default-credentials probe. Substrate defaults this
  # to true (for GKE + Artifact Registry); on kind it just adds latency and a
  # noisy log line before falling back to anonymous auth.
  gcpAuthForImagePulls: false

  # Rewrite `localhost:PORT/...` image refs (which is what `make helm-install`
  # renders when `--set registry=localhost:5001` is in effect) to a hostname
  # that atelet's puller can actually resolve from inside its pod. atelet also
  # applies `name.Insecure` for any ref that was originally `localhost:*`, so
  # the rewritten `kind-registry:5000` ref is fetched over HTTP (kind-registry
  # is `registry:2` with no TLS by default). Without both parts of this — the
  # rewrite AND the insecure flag it triggers — atelet errors out with either
  #   `dial tcp [::1]:5001: connect: connection refused`  (no rewrite), or
  #   `http: server gave HTTP response to HTTPS client`   (rewrite without Insecure).
  extraArgs:
    - --localhost-registry-replacement=kind-registry:5000
```

When installing Substrate as a **subchart** of kagent (i.e. `--set substrate.enabled=true`
on the kagent chart), prefix these keys with `substrate.` — e.g.
`--set-json 'substrate.atelet.extraArgs=["--localhost-registry-replacement=kind-registry:5000"]'`.

Then install the Substrate platform and kagent:

```bash
export SUBSTRATE_VERSION=0.0.7

helm upgrade --install substrate-crds \
  oci://ghcr.io/kagent-dev/substrate/helm/substrate-crds \
  --version "${SUBSTRATE_VERSION}"

helm upgrade --install substrate \
  oci://ghcr.io/kagent-dev/substrate/helm/substrate \
  --version "${SUBSTRATE_VERSION}" \
  --namespace ate-system \
  --create-namespace -f substrate-values.yaml

make helm-install KAGENT_HELM_EXTRA_ARGS="\
  --set controller.substrate.enabled=true \
  --set controller.substrate.ateApiEndpoint=dns:///api.ate-system.svc:443 \
  --set controller.substrate.ateApiInsecure=true \
  --set substrateWorkerPool.create=true \
  --set substrateWorkerPool.ateomImage=ghcr.io/kagent-dev/substrate/ateom-gvisor:v${SUBSTRATE_VERSION}"
```

When `substrateWorkerPool.create=true`, the kagent chart installs a namespace-scoped `WorkerPool` with:

- `spec.sandboxClass: gvisor`
- label `kagent.dev/worker-pool: kagent-default` (matches generated `ActorTemplate` selectors)
- controller default `workerPool` name set to that pool when `create=true`

**Zero-downtime rollouts:** SandboxAgent config and image rollouts retain the previous `ActorTemplate` until the new golden is Ready (blue-green). Use `substrateWorkerPool.replicas: 2` or higher so a spare worker can build the new golden while the current one keeps serving chat.

## 2. AgentHarness with Substrate runtime

kagent generates a per-harness `ActorTemplate` and schedules actors onto an existing `WorkerPool`.

Create a harness. If `snapshotsConfig` is omitted, kagent defaults it to `gs://ate-snapshots/<namespace>/<agentharnessname>`.

- **Worker pool** — reference an existing pool (`workerPoolRef`) or configure a controller default WorkerPool. The target pool must carry label `kagent.dev/worker-pool: <pool-name>`. The kagent Helm-managed pool gets this label automatically; externally owned pools must add it manually.

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

    # Required unless the controller has a default WorkerPool configured.
    workerPoolRef:
      name: kagent-default

    # Optional: override the sandbox image used in the ActorTemplate (must be digest-pinned).
    # workloadImage: ghcr.io/kagent-dev/nemoclaw/sandbox-base@sha256:d52bee415dc4c0dba7164f9eabe727574c056d4f211781f20af249707883a3b4
```

kagent creates an `ActorTemplate` that looks roughly like this:

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
  sandboxClass: gvisor
  workerSelector:
    matchLabels:
      kagent.dev/worker-pool: kagent-default
  snapshotsConfig:
    location: gs://ate-snapshots/kagent/peterj-claw
  containers:
  - name: openclaw
    image: ghcr.io/kagent-dev/nemoclaw/sandbox-base@sha256:d52bee415dc4c0dba7164f9eabe727574c056d4f211781f20af249707883a3b4
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

The generated `command` contains a base64-encoded `openclaw.json`, so the live object will be more verbose than the abbreviated example above. `pauseImage`, runsc URLs and hashes, and the default workload image come from controller/Helm configuration unless overridden on the `AgentHarness`. kagent also sets `gateway.controlUi.basePath` to `/api/agentharnesses/<namespace>/<name>/gateway` so OpenClaw serves the Control UI under the same path kagent proxies.

When `modelConfigRef` or `spec.channels` are set, credentials are **not** copied into the ActorTemplate or `openclaw.json` as plaintext. kagent writes `valueFrom.secretKeyRef` (or inline `value` for harness inline tokens) on the ActorTemplate container env; Substrate `ate-api` resolves those refs at actor resume. In `openclaw.json`, kagent uses OpenClaw [env SecretRefs](https://docs.openclaw.ai/gateway/secrets) (`{source:"env",provider:"default",id:"<VAR>"}`) for `models.providers.*.apiKey`, `channels.telegram.accounts.*.botToken`, and `channels.slack.accounts.*.botToken` / `appToken`. Rotate a Secret and recreate the ActorTemplate golden snapshot when keys change.

With `controller.substrate.enabled=true`, the kagent Helm chart installs a namespace-scoped Role and RoleBinding so `ate-api-server` (in `ate-system` by default) can `get` Secrets and ConfigMaps referenced by generated ActorTemplates. Harnesses in other namespaces need that namespace listed in `rbac.namespaces` (or a matching RoleBinding applied manually).

Port-forward the UI:

```bash
kubectl port-forward -n kagent svc/kagent-ui 8001:8080
```

Navigate to the deployed agent harness. If the OpenClaw Control UI asks for a gateway connection, use:

- Gateway URL: `http://localhost:8001/api/agentharnesses/kagent/peterj-claw/gateway/`

The gateway URL must include the trailing slash. The gateway runs without authentication; the actor's only externally reachable surface is reached through the controller's same-origin proxy over the actor's private atenet ingress.

kagent proxies UI traffic to the actor OpenClaw gateway through Substrate's **atenet-router** (Envoy) using the actor `Host` header (`<actor-id>.actors.resources.substrate.ate.dev`). The default router URL is `http://atenet-router.ate-system.svc:80`; override with `controller.substrate.atenetRouterURL` when needed.
