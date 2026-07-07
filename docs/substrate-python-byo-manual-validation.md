# Manual validation: Python runtime + BYO (Go/Python) on Agent Substrate

This is a self-contained runbook to validate that substrate `SandboxAgent`s can run as
**Declarative Python**, **Declarative Go** (regression), **BYO Go**, and **BYO Python**, using a
clean local kind cluster. It tears everything down at the end.

> Implementation reference: `SUBSTRATE_PYTHON_BYO_PLAN.md` (root of the repo).

---

## What this validates

| # | Check | Needs |
|---|-------|-------|
| 1 | CRD admission accepts Declarative **Python** on substrate | kagent CRDs |
| 2 | CRD admission accepts **BYO** on substrate (previously rejected) | kagent CRDs |
| 3 | BYO **without** an explicit command is rejected | controller validation |
| 4 | Controller generates a correct **ActorTemplate** (image + command + env) per runtime/type | kagent controller + substrate CRDs |
| 5 | Golden snapshot completes + actor reaches Ready (declarative) | substrate control plane + gVisor + a worker slot |

**Environment notes (confirmed on Docker Desktop + kind, 2026-06-17):**
- Substrate **v0.0.6 bundles `rustfs`** (an S3-compatible store) for snapshots, so **no external
  GCS is required** for a local run — golden snapshots complete locally.
- gVisor (`atelet`) runs actors fine under kind on Docker Desktop.
- The default `substrateWorkerPool` has **`replicas: 1`** → only one actor can resume at a time.
  Running several agents at once yields `no free workers available`; scale the pool
  (`--set substrateWorkerPool.replicas=N`) to validate multiple concurrently.
- BYO actor-resume needs a **real self-contained A2A image serving :80**. Checks 1–4 fully
  validate BYO (admission, cmd-validation, ActorTemplate generation, verbatim command handoff to
  `atelet`); BYO reaching `Ready` additionally needs that real image + a free worker.

---

## 0. Prerequisites

- `docker`, `kind` (≥ v0.30), `kubectl`, `helm` (≥ v3.16).
- A working Docker daemon that can **start new containers** (see Troubleshooting).
- `export OPENAI_API_KEY=sk-...` (the default provider gate; declarative agents need a model config).
- No external GCS needed — substrate v0.0.6 bundles `rustfs` for snapshot storage.

---

## 1. Clean setup of the `kagent` kind cluster

```bash
cd <repo-root>

# Optional but recommended for a truly clean slate (prunes old kagent images,
# the kagent buildx builder, and the kind node image cache; leaves other clusters alone):
make clean

# Create the kind cluster 'kagent' + local registry (localhost:5001) + MetalLB:
make create-kind-cluster
make use-kind-cluster   # merges kubeconfig, sets context kind-kagent, namespace kagent
```

Verify:

```bash
kubectl --context kind-kagent get nodes      # control-plane Ready
```

---

## 2. Install Agent Substrate (ate-system)

From `examples/substrate-openclaw/README.md`:

```bash
cat > /tmp/substrate-values.yaml <<'EOF'
atelet:
  extraArgs:
    - --localhost-registry-replacement=kind-registry:5000
EOF

export ATEOM_VERSION=v0.0.6
helm upgrade --install substrate-crds oci://ghcr.io/kagent-dev/substrate/helm/substrate-crds
helm upgrade --install substrate oci://ghcr.io/kagent-dev/substrate/helm/substrate \
  --namespace ate-system --create-namespace -f /tmp/substrate-values.yaml
```

Verify the substrate CRDs exist (needed for the controller to create ActorTemplates):

```bash
kubectl get crd | grep -E 'actortemplates|workerpools|actors'   # ate.dev CRDs
```

---

## 3. Build + install kagent with substrate enabled

This builds all images (including the new **distroless slim** and **full** Python images) and
installs kagent. The controller embeds the runtime image digests at build time, so it must be
rebuilt whenever those images change.

```bash
make helm-install KAGENT_HELM_EXTRA_ARGS="\
  --set controller.substrate.enabled=true \
  --set controller.substrate.ateApiEndpoint=dns:///api.ate-system.svc:443 \
  --set controller.substrate.ateApiInsecure=true \
  --set substrateWorkerPool.create=true \
  --set substrateWorkerPool.ateomImage=ghcr.io/kagent-dev/substrate/ateom-gvisor:${ATEOM_VERSION}"
```

Verify:

```bash
kubectl -n kagent get pods                        # controller + ui Running
kubectl -n kagent get modelconfig                 # default-model-config present
kubectl -n kagent get workerpool                  # kagent-default (may be Pending without gVisor)
```

Confirm the **new image split** shipped (optional sanity):

```bash
# slim image must be distroless (no shell):
docker run --rm --entrypoint sh localhost:5001/kagent-dev/kagent/app:$(grep -m1 VERSION Makefile) -c true ; echo "exit=$? (non-zero = distroless, expected)"
```

---

## 4. Validation manifests

```bash
mkdir -p /tmp/kagent-validation && cd /tmp/kagent-validation
```

**a) Declarative Python on substrate** (`py-decl-substrate.yaml`):

```yaml
apiVersion: kagent.dev/v1alpha2
kind: SandboxAgent
metadata: { name: py-decl-substrate, namespace: kagent }
spec:
  type: Declarative
  platform: substrate
  declarative:
    runtime: python
    modelConfig: default-model-config
    systemMessage: "You are a helpful Python-runtime substrate test agent."
  substrate:
    workerPoolRef: { name: kagent-default }
```

**b) Declarative Go on substrate** (regression — `go-decl-substrate.yaml`): same as (a) with
`runtime: go`.

**c) BYO Go on substrate** (`byo-go-substrate.yaml`) — note the image **must be digest-pinned**
and the command **must be explicit**, and the process must serve A2A on **port 80**:

```yaml
apiVersion: kagent.dev/v1alpha2
kind: SandboxAgent
metadata: { name: byo-go-substrate, namespace: kagent }
spec:
  type: BYO
  platform: substrate
  byo:
    deployment:
      image: "<your-byo-go-a2a-image>@sha256:<digest>"   # MUST be digest-pinned
      cmd: "/app"                                          # explicit cmd REQUIRED
      args: ["--host", "0.0.0.0", "--port", "80"]          # serve on :80
  substrate:
    workerPoolRef: { name: kagent-default }
```

**d) BYO Python on substrate** (`byo-py-substrate.yaml`) — **validated, reaches `Ready`.** This
uses the repo's `python/samples/langgraph/kebab` sample (a self-contained Python A2A server).
First build + push it and capture its digest:

```bash
docker buildx build --builder kagent-builder-v0.23.0 --push \
  --platform linux/$(uname -m | sed 's/aarch64/arm64/;s/x86_64/amd64/') \
  -t localhost:5001/langgraph-kebab:latest -f python/samples/langgraph/kebab/Dockerfile ./python
DIG=$(curl -fsS -H "Accept: application/vnd.oci.image.index.v1+json" -o /dev/null -D - \
  http://127.0.0.1:5001/v2/langgraph-kebab/manifests/latest \
  | awk 'tolower($1)=="docker-content-digest:"{gsub("\r","",$2);print $2}')
sed "s#<DIG>#${DIG}#" byo-py-substrate.yaml | kubectl apply -f -
```

```yaml
# byo-py-substrate.yaml
apiVersion: kagent.dev/v1alpha2
kind: SandboxAgent
metadata: { name: byo-py-substrate, namespace: kagent }
spec:
  type: BYO
  platform: substrate
  byo:
    deployment:
      # localhost:5001 (NOT kind-registry:5000) so atelet pulls over HTTP — see Gotchas.
      image: "localhost:5001/langgraph-kebab@<DIG>"
      cmd: "/app/.venv/bin/python"                  # absolute path (no shell/PATH/cwd in substrate)
      args: ["samples/langgraph/kebab/kebab/cli.py"]
      env:
        - { name: KAGENT_URL, value: "http://kagent-controller.kagent:8083" }
        - { name: PORT, value: "80" }               # actor is reached on :80
        - { name: HOST, value: "0.0.0.0" }
        - { name: OPENAI_API_KEY, value: "<key>" }  # this sample builds a ChatOpenAI at import
  substrate:
    workerPoolRef: { name: kagent-default }
```

Observed: `Ready=True / ActorTemplate golden snapshot is ready`. (`KAGENT_NAME`/`KAGENT_NAMESPACE`
are auto-injected by kagent's substrate path.)

**c'/d' note — BYO Go:** the (c) manifest above is the BYO Go shape; to take it all the way to
`Ready` you need a self-contained Go A2A image that serves `/health` on :80 (the repo's Go kebab
e2e agent currently can't build on the distroless base — see Gotcha 5). BYO Python (this section)
is the recommended path to a fully-`Ready` BYO demo.

**e) Negative — BYO without a command** (`byo-nocmd-substrate.yaml`):

```yaml
apiVersion: kagent.dev/v1alpha2
kind: SandboxAgent
metadata: { name: byo-nocmd-substrate, namespace: kagent }
spec:
  type: BYO
  platform: substrate
  byo:
    deployment:
      image: "<your-byo-image>@sha256:<digest>"   # no cmd on purpose
  substrate:
    workerPoolRef: { name: kagent-default }
```

---

## 5. Run the checks

### Check 1 & 2 — CRD admission accepts Python + BYO

```bash
kubectl apply -f py-decl-substrate.yaml      # expect: created (Python on substrate)
kubectl apply -f go-decl-substrate.yaml      # expect: created (Go regression)
kubectl apply -f byo-go-substrate.yaml       # expect: created (BYO now allowed)
kubectl apply -f byo-py-substrate.yaml       # expect: created
```

Before this change, the BYO ones were rejected by the CRD with
`BYO agents are not supported when spec.platform is substrate`. They should now apply cleanly.

### Check 3 — BYO without a command is rejected

```bash
kubectl apply -f byo-nocmd-substrate.yaml
# Expect the agent's Accepted condition to go False with:
#   "BYO agents on substrate must set spec.byo.deployment.cmd ..."
kubectl -n kagent get sandboxagent byo-nocmd-substrate -o jsonpath='{.status.conditions}' | jq
```

### Check 4 — Generated ActorTemplate is correct per runtime/type

```bash
for a in py-decl-substrate go-decl-substrate byo-go-substrate byo-py-substrate; do
  echo "===== $a ====="
  kubectl -n kagent get actortemplate "$a" -o jsonpath='{.spec.containers[0].image}{"\n"}{.spec.containers[0].command}{"\n"}' 2>/dev/null
done
```

Expected `command`:
- **Go declarative:** `["/app","--host","0.0.0.0","--port","80"]`
- **Python declarative:** `["/.kagent/.venv/bin/kagent-adk","static","--host","0.0.0.0","--port","80"]`
- **BYO:** the container's explicit `cmd` + `args`, verbatim.

Expected `image`:
- **Python declarative:** the `app` image digest (the **full** variant if the agent uses skills /
  `executeCodeBlocks`, otherwise the **slim** distroless variant).
- **Go declarative:** the `golang-adk` (or `-full`) image digest.
- **BYO:** the user's digest-pinned image.

Declarative ActorTemplates should also carry the secret-backed config env
(`KAGENT_CONFIG_JSON`, `KAGENT_AGENT_CARD_JSON`); BYO should not.

```bash
kubectl -n kagent get actortemplate py-decl-substrate \
  -o jsonpath='{range .spec.containers[0].env[*]}{.name}{"\n"}{end}' | grep KAGENT_CONFIG_JSON
```

### Check 5 — Golden snapshot + actor + A2A (environment-gated)

```bash
kubectl -n kagent get actortemplate -o wide      # Phase: Ready when golden snapshot done (needs GCS)
kubectl -n kagent get sandboxagent               # Ready=True when actor is reachable
# A2A round-trip via the UI:
kubectl -n kagent port-forward svc/kagent-ui 8001:8080
# open http://localhost:8001 and chat with py-decl-substrate
```

If `WorkerPool`/`ActorTemplate` stay `Pending`, you are blocked at the GCS/gVisor ceiling —
checks 1–4 are still valid evidence that the feature works end-to-end through ActorTemplate
generation.

### Check 6 — Orphaned session actors are reaped on config rollout

Validates that when **any** chat session resumes after a config rollout, the controller sweeps and
deletes **all** of the agent's session actors left under superseded ActorTemplates (rather than
letting them linger until each session/the agent is deleted). Goldens and live (current-template)
actors are left intact.

```bash
# 1. Chat with py-decl-substrate under a few different contextIds (creates session actors under the
#    current hash). Inspect actors via ate-api (grpcurl against api.ate-system with a minted
#    `kubectl create token kagent-controller --audience api.ate-system.svc`), filtering to `asr-*`.

# 2. Roll the config so a new golden/template is built, then wait for it to reach Ready:
kubectl patch sandboxagent py-decl-substrate -n kagent --type=merge \
  -p '{"spec":{"declarative":{"systemMessage":"You are a rollout-test agent (v2)."}}}'
watch kubectl get actortemplate -n kagent          # new <name>-<hash> appears, builds to Ready

# 3. Chat in ONE session again. EnsureSessionActor creates that session's actor under the new hash;
#    a background reaper then sweeps ALL of the agent's SUSPENDED session actors under the old
#    (superseded) templates — including OTHER sessions' actors — and deletes them.
# 4. List the agent's `asr-*` actors again: every session actor under a superseded template that was
#    SUSPENDED is gone; current-template actors and all goldens (UUID ids) remain.
```

Expected: after step 3 the agent's `asr-*` session actors under superseded templates that were
SUSPENDED are all deleted (not just the resuming session's); actors under the current template and
the golden actors (UUID ids) are kept. A RUNNING orphan (mid-request) is left to quiesce and reaped
on a later pass.

**Live result (2026-06-29, kind-kagent, controller built with the reap change): PASS.** Against
`test-decl-substrate` (Python declarative). Current template `…-cf724b7e15b9b5d4` (gen 6, Ready);
baseline session actors (`asr-*`), all SUSPENDED and all under **superseded** templates:
`asr-46893…` + `asr-65828…` under `fb72a5…` (gen 5), and `asr-a4d27…` under `e9970…` (gen 4).
1. Chatted a **brand-new** session `reapsweep-001` → `EnsureSessionActor` created its actor under
   the current `cf724b…` template and the detached reaper swept the agent's orphans.
2. Verified via `ateapi.Control/GetActor`: `asr-65828…` and `asr-a4d27…` → `NotFound` (reaped — note
   these belong to **different** sessions than the one that resumed); the new `cf724b…` actor →
   present; all five per-template **golden** actors (UUID ids) → `STATUS_SUSPENDED` (untouched).
3. `asr-46893…` (same superseded template as the reaped `asr-65828…`) survived the first sweep —
   ate-api's `ListActors` is **eventually-consistent**, so that pass's snapshot didn't include it.
   Chatting a second new session (`reapsweep-002`) triggered another sweep and `asr-46893…` →
   `NotFound`. The reaper is idempotent and converges across successive creates.

Final state: only the two new `reapsweep-*` actors remain — both under the **current** `cf724b…`
template (correctly kept) — every superseded-template orphan is gone, all goldens intact, and the
agent stayed `Ready=True` throughout (regression clean).

> Two takeaways: (1) an earlier iteration scoped the reap to only the resuming session's own orphan;
> it was broadened (this Check) to sweep **all** of the agent's superseded-template session actors
> whenever any session resumes post-rollout. (2) Because `ListActors` is eventually-consistent, a
> single sweep cleans what it sees and any stragglers are reaped on the next session create — the
> sweep is best-effort and idempotent, not a single-shot guarantee.

---

## 6. Teardown

```bash
kubectl -n kagent delete sandboxagent \
  py-decl-substrate go-decl-substrate byo-go-substrate byo-py-substrate byo-nocmd-substrate --ignore-not-found

helm uninstall substrate -n ate-system || true
helm uninstall substrate-crds || true

make delete-kind-cluster        # deletes the kind 'kagent' cluster
docker rm -f kind-registry || true   # remove the local registry container
```

---

## Troubleshooting

- **kind node container stuck in `created` / `kind create` hangs; `docker run hello-world`
  never prints "Hello".** Docker Desktop is in a state where it cannot *start* new containers
  (existing long-running ones keep working). This blocks cluster creation entirely. Fix: **restart
  Docker Desktop**, then re-run from step 1. (Observed during authoring on 2026-06-16; this is a
  Docker daemon issue, not a kagent issue.)
- **`OPENAI_API_KEY ... not set`** from `make helm-install`: export the key for the default
  provider (or set `KAGENT_DEFAULT_MODEL_PROVIDER` + the matching key).
- **`workload image ... must be pinned with a digest`** on a BYO substrate agent: substrate
  copies the command verbatim and requires a digest-pinned image — use `image@sha256:...`.
- **BYO actor never becomes reachable** even with gVisor/GCS: confirm the BYO process listens on
  **port 80** (substrate routes to the actor on :80; the usual 8080 will not be reached).
- **Python agent: chat returns nothing; worker log shows `FATAL ERROR: ... inconsistent private
  memory files on restore: savedMFOwners = [pause:/], mfmap = map[kagent:/...]`.** This gVisor
  restore error is a *symptom*, not the cause. The real failure is earlier, in the **golden
  actor's** logs (search the worker pod for the golden actor ID): the Python process crashed on
  startup with `ImportError: libz.so.1: cannot open shared object file` (numpy C-extension). Root
  cause: **substrate builds the OCI `Process.Env` from a hardcoded `PATH` + the ActorTemplate env
  only — it ignores the image's `ENV` directives** (the same way it ignores the image entrypoint;
  see `cmd/atelet/oci.go` `prepareOCIDirectory`). The Python image relies on
  `ENV LD_LIBRARY_PATH=/usr/lib/kagent-libs` to find its bundled shared libs, so under substrate
  that path is unset and numpy fails to load `libz.so.1`. The crashed `kagent` container leaves
  only `pause` memory-resident, so the golden snapshot captures `pause` only and the
  template still goes `Ready` (misleading) — but every restore then fails the MemoryFile
  consistency check. **Fix (in-tree):** the controller re-supplies the Python runtime image's
  ENV (`LD_LIBRARY_PATH`, `VIRTUAL_ENV`, `PYTHONUNBUFFERED`, `LANG`, `LC_ALL`) via the
  ActorTemplate for declarative Python (`buildSubstrateKagentContainerCommand` →
  `pythonRuntimeImageEnv`). Verify with:
  `kubectl get actortemplate <name> -n <ns> -o jsonpath='{range .spec.containers[0].env[?(@.name=="LD_LIBRARY_PATH")]}{.value}{end}'`.

> **Substrate contract (important):** substrate does **not** apply an image's `ENV` directives,
> only its hardcoded `PATH` plus the ActorTemplate env. This is in addition to the known
> "no image-entrypoint fallback" rule. **Declarative Python** is handled in-tree (the controller
> re-supplies the needed ENV). **BYO** images on substrate must likewise not rely on baked-in
> `ENV`: pass any runtime-required env explicitly via `spec.byo.deployment.env`.

---

## Status of this runbook — executed live 2026-06-17

This runbook was run end-to-end on a clean kind `kagent` cluster (Docker Desktop) and then torn
down. Observed results:

- **Check 1 & 2 (CRD admission): PASS.** `py-decl-substrate` (Declarative/python),
  `go-decl-substrate` (Declarative/go), `byo-go-substrate` and `byo-py-substrate` (BYO) all
  applied cleanly on `platform: substrate`.
- **Check 3 (BYO without cmd): PASS.** `byo-nocmd-substrate` → `Accepted=False`,
  message `BYO agents on substrate must set spec.byo.deployment.cmd (substrate does not fall back
  to the image entrypoint)`.
- **Check 4 (ActorTemplate generation): PASS.** Observed commands:
  - Python decl → `["/.kagent/.venv/bin/kagent-adk","static","--host","0.0.0.0","--port","80"]`,
    image = the **slim** `app` digest.
  - Go decl → `["/app","--host","0.0.0.0","--port","80"]`, image = `golang-adk` digest.
  - BYO → the container's verbatim cmd+args, image = the user's pinned image.
  - Python decl carried `KAGENT_CONFIG_JSON` env; BYO did not.
- **Check 5 (golden snapshot + actor): PASS for declarative AND BYO.** `py-decl-substrate`,
  `go-decl-substrate`, and a real BYO agent (`byo-langgraph-substrate`, see the recipe below) all
  reached `Ready=True` (`ActorTemplate golden snapshot is ready`) using the bundled `rustfs`
  (no GCS).
- **Check 7 (config-change propagation — new template per config, previous retained): PASS (re-validated 2026-06-19, single-replica WorkerPool).**
  Editing `spec.declarative.modelConfig` generates a NEW config-hashed ActorTemplate (`<name>-<hash>`)
  with a fresh golden; the previous template **keeps serving until the new golden is Ready**, then the
  agent flips to the new model. The previous template + golden are now **retained, not retired** — they
  are stateful and pin no workers (a suspended actor frees its worker), and are cleaned up only when the
  SandboxAgent itself is deleted. Verified by flipping `google-model-config` (Gemini) →
  `default-model-config` (OpenAI): during the rebuild both templates were present — the old
  (gemini, `...-16cf0ceb4f243467`) stayed `phase: Ready` and the SandboxAgent `Ready` condition stayed
  **True** the whole time — then the new (openai, `...-7f52a218751d4f4f`) reached Ready and the agent
  flipped to `default-model-config`. **Both templates and their goldens remained `Ready` afterward**
  (no deletion across ~50s of subsequent reconcile cycles). No downtime gap; done on **1 worker**.
  To reproduce: `kubectl patch sandboxagent <name> -n kagent --type=merge -p '{"spec":{"declarative":{"modelConfig":"google-model-config"}}}'`,
  then `watch kubectl get actortemplate -n kagent` — you'll see the new template build to `Ready`
  **alongside** the old one (which now stays); `kubectl get sandboxagent <name> -n kagent -o jsonpath='{.status.conditions}'`
  stays `Ready=True` throughout. Chat to confirm the model switched.
  Mechanics: golden snapshots are immutable and substrate snapshots once, so a config change must
  create a new template (name carries `kagent.dev/config-hash`); `ResolveCurrentActorTemplate` returns
  the newest **Ready** template, so chat/readiness flip atomically once the new golden is Ready.
  Superseded templates, goldens, and per-session actors are **retained** (suspended → zero workers) and
  removed only on SandboxAgent delete (`DeleteAllSandboxAgentActors` + `CleanupSandboxAgentTemplate`,
  which iterate all of the agent's templates/goldens). Session ids fold in the hash so new chats use the
  new golden; session history is preserved (persisted in the kagent DB, not the actor).
  **Capacity behavior (no buffering):** the chat path does not buffer/retry on worker contention. On a
  **single-replica** pool the lone worker is briefly occupied building the new golden, so a chat arriving
  mid-build returns `ErrNoFreeWorkers` ("…increase WorkerPool replicas") immediately rather than waiting;
  on a **multi-replica** pool the spare workers keep serving the previous golden during the build, so a
  rollout does not surface that error. Scaling the WorkerPool is the remedy for capacity pressure. If a
  worker is ever pinned by a stuck/orphaned RUNNING actor, clear it with
  `kubectl rollout restart deploy/kagent-default-deployment -n kagent`.
- **Check 6 (Python actor restore + A2A round-trip): PASS (re-validated 2026-06-17).** Initial
  chat against `py-decl-substrate` failed: the actor would not restore (gVisor `inconsistent
  private memory files` — see Troubleshooting). Root-caused to substrate dropping the image
  `ENV`, so the Python runtime's `LD_LIBRARY_PATH` was unset and numpy crashed on `libz.so.1`
  during golden creation. After the controller fix (`pythonRuntimeImageEnv` re-supplies the ENV
  via the ActorTemplate) and recreating the agent, the golden actor's Python started cleanly
  (`Uvicorn running on http://0.0.0.0:80`), the snapshot captured a healthy container, and an
  A2A `message/send` (with a `contextId`) **restored an actor and executed the agent end-to-end**
  — the request reached the LLM call (only a `429 insufficient_quota` from OpenAI, an
  environment/credentials limit, prevented a final answer). No `inconsistent private memory`
  errors after the fix. Note: the single-replica WorkerPool must have a free worker for the golden
  resume — scale `kagent-default` to ≥2 if a stale actor holds the slot.

Code-level validation also green: Go unit tests (api / substrate / translator), Python
materialization tests, UI jest suite (264 tests), and local `docker build` of the slim + full
Python images.

### Validated BYO recipe that reaches `Ready` (uses the repo's `langgraph-kebab` sample)

The `python/samples/langgraph/kebab` sample is a self-contained A2A server (`/health` + agent
card). Build and push it, then deploy as BYO:

```bash
# Build just the langgraph-kebab sample to the kind registry (NOT `make push-test-agent`,
# which also builds the Go kebab agent that currently breaks on the distroless base — see Gotchas):
docker buildx build --builder kagent-builder-v0.23.0 --push --platform linux/$(uname -m | sed 's/aarch64/arm64/;s/x86_64/amd64/') \
  -t localhost:5001/langgraph-kebab:latest -f python/samples/langgraph/kebab/Dockerfile ./python
DIG=$(curl -fsS -H "Accept: application/vnd.oci.image.index.v1+json" -o /dev/null -D - \
  http://127.0.0.1:5001/v2/langgraph-kebab/manifests/latest | awk 'tolower($1)=="docker-content-digest:"{gsub("\r","",$2);print $2}')
```

```yaml
apiVersion: kagent.dev/v1alpha2
kind: SandboxAgent
metadata: { name: byo-langgraph-substrate, namespace: kagent }
spec:
  type: BYO
  platform: substrate
  byo:
    deployment:
      # IMPORTANT: reference via localhost:5001 (NOT kind-registry:5000) — see Gotchas.
      image: "localhost:5001/langgraph-kebab@<DIG>"
      cmd: "/app/.venv/bin/python"                       # absolute path; substrate has no cwd/PATH/shell
      args: ["samples/langgraph/kebab/kebab/cli.py"]
      env:
        - { name: KAGENT_URL, value: "http://kagent-controller.kagent:8083" }
        - { name: PORT, value: "80" }                    # substrate routes to the actor on :80
        - { name: HOST, value: "0.0.0.0" }
        - { name: OPENAI_API_KEY, value: "<key>" }       # this sample builds a ChatOpenAI at import
  substrate:
    workerPoolRef: { name: kagent-default }
```

`KAGENT_NAME`/`KAGENT_NAMESPACE` are injected automatically by kagent's substrate path; the
sample additionally needs `KAGENT_URL` and (for langgraph) `OPENAI_API_KEY` to start, plus
`PORT=80`. Observed: `Ready=True / ActorTemplate golden snapshot is ready`.

### Gotchas (discovered during live validation)
1. **BYO image must be referenced via `localhost:5001/...`, not `kind-registry:5000/...`.**
   `atelet` is started with `--localhost-registry-replacement=kind-registry:5000`, which rewrites
   `localhost:5001` refs to the in-cluster registry **and pulls them over HTTP**. A direct
   `kind-registry:5000` ref skips that and fails with
   `http: server gave HTTP response to HTTPS client`.
2. **BYO command must be absolute and self-sufficient.** Substrate copies the command verbatim
   into the OCI spec — no shell, no `$PATH` resolution, and cwd is not the image `WORKDIR`. Use
   absolute binary paths (`/app/.venv/bin/python`) and either absolute script paths or scripts
   that resolve their own location.
3. **Listen on port 80.** The actor is reached on :80; an image defaulting to 8080 must be
   reconfigured (here via `PORT=80`).
4. **Size the WorkerPool.** `substrateWorkerPool.replicas` must be ≥ the number of concurrent
   actors, or extra agents report `no free workers available`. The default is 1; this run used 3.
5. **`make push-test-agent` currently fails** on the Go kebab agent
   (`go/core/test/e2e/agents/kebab/Dockerfile` does `FROM kagent-adk` + `RUN uv sync`, which no
   longer works on the distroless slim base). Build the sample you need directly (as above), or
   repoint that Dockerfile at `kagent-adk-full`. Tracked as a follow-up.
