# Plan: Python runtime + BYO (Go/Python) on Substrate, and Python image migration off Chainguard

## Context

Today kagent's **Agent Substrate** (ate.dev) integration is deliberately narrow (introduced in PR #1981):

- A `SandboxAgent` with `spec.platform: substrate` may only be **Declarative**, and its runtime is **force-set to Go** (`EffectiveDeclarativeRuntimeForAgent`, `go/api/v1alpha2/agent_types.go:286-294`). Python and BYO are rejected by validation.
- This was *initial scoping*, not a hard platform limitation — Go was simply the first runtime that worked end-to-end on substrate (static binary, single explicit entrypoint, config materialized from env). There is no known gVisor snapshot/restore blocker for Python.

Separately, the **Python agent runtime image** (`python/Dockerfile`) is built on `cgr.dev/chainguard/wolfi-base:latest`. Chainguard's free tier only ships `:latest` and rotates digests, so we **cannot pin** it and it has been unreliable. The Go runtime image already uses `gcr.io/distroless/static:nonroot` (slim) + a wolfi `Dockerfile.full` for code-execution agents — a proven two-image pattern we will mirror for Python.

**Goal:** Allow substrate SandboxAgents to run as **Declarative Python**, **BYO Go**, and **BYO Python**, and migrate the Python image to a **digest-pinnable distroless** base (slim) plus a pinned **full** image for sandbox/code-execution agents.

**Decisions (confirmed with user):**
- Python image: **Option A — split** distroless slim + pinned full (mirrors Go).
- Go-forcing on substrate was just initial scope → safe to expand.
- One combined, phased plan.
- Skills on substrate stay **unsupported** (out of scope; keep that validation).

---

## Phase 1 — Split the Python image (distroless slim + pinned full)

Mirror the Go pattern: `go/Dockerfile` (distroless) + `go/Dockerfile.full` (wolfi w/ bash, nodejs, bubblewrap, socat).

**`python/Dockerfile` → distroless slim** (no sandbox-runtime):
- Multi-stage: a builder stage (keep uv + a standard base such as `debian:12-slim` pinned by digest) installs the uv-managed standalone Python (`/python`) and the project venv (`/.kagent/.venv`) exactly as today (`uv venv` + `uv sync --package kagent-adk`, lines 118-123).
- Final stage `FROM gcr.io/distroless/cc-debian12:nonroot` (the **cc** variant — provides glibc + `libstdc++`, which the current image installs at line 19/22). `COPY` the standalone interpreter (`/python`) and venv (`/.kagent/.venv`) from the builder, plus CA certs.
- Drop from the slim image: nodejs/npm/node-gyp/bubblewrap/socat/ripgrep (lines 68-91), the bash sandbox venv (`/.kagent/sandbox-venv`, lines 125-135), and `bash`/`git`/`curl`.
- Entrypoint must be **exec-form with an absolute path** (no shell in distroless): `ENTRYPOINT ["/.kagent/.venv/bin/kagent-adk", "run", "--host", "0.0.0.0", "--port", "8080"]`. Verify the venv console-script shebang resolves to the copied `/python` interpreter.

**`python/Dockerfile.full` (NEW)** — for agents needing in-container code execution / bash tools:
- Base on a **digest-pinnable** image (e.g. `debian:12-slim@sha256:…`, installing tooling via apt) rather than `chainguard:latest`, to fix the pinning problem. Carry over the SRT install (nodejs, bubblewrap, socat, ripgrep), the Anthropic sandbox-runtime build (lines 77-91), and the bash sandbox venv (lines 125-135).
- Same `kagent-adk run` entrypoint.

**Build/wiring:**
- `Makefile` — add a full Python image target + tag (mirror `GOLANG_ADK_FULL_IMG`, lines ~56-73). Build/push both slim and full.
- `scripts/controller-digest-ldflags.sh` — add `PythonADKFullImageDigest` alongside `PythonADKImageDigest` (lines 98-100).
- `go/core/internal/controller/translator/agent/adk_api_translator.go` — add `var PythonADKFullImageDigest string` next to the existing digest vars (~lines 121-123).

> Note in the PR description any base-image digests that are pinned, so the "can't pin" problem is visibly resolved.

---

## Phase 2 — Python ADK: materialize config from env (substrate parity)

Substrate copies the container `Command` verbatim and injects agent config via **secret-backed env vars** (`KAGENT_CONFIG_JSON`, `KAGENT_AGENT_CARD_JSON`, `KAGENT_SRT_SETTINGS_JSON`). The Go ADK handles this in `go/adk/pkg/config/config_materialize.go` (`MaterializeFromEnv`, called from `go/adk/cmd/main.go:84`). **The Python ADK has no equivalent** — `cli.py` reads only ad-hoc env vars.

- Add a Python equivalent of `MaterializeFromEnv` in `python/packages/kagent-adk/src/kagent/adk`: read those three env vars, decode (match the Go encoding — confirm base64-vs-raw in `config_materialize.go`), write them into the config dir the `run`/`static` path already loads via `--filepath`/`KAGENT_SKILLS_FOLDER`.
- Wire it into the `run` command in `python/packages/kagent-adk/src/kagent/adk/cli.py` so that when the env vars are present, config is materialized before the server starts.
- Add Python unit tests for the materialization (present, absent, malformed).

---

## Phase 3 — Remove substrate runtime/BYO constraints (API layer)

- `go/api/v1alpha2/agent_types.go:286-294` — `EffectiveDeclarativeRuntimeForAgent`: **remove the substrate Go-forcing branch** so substrate declarative agents honor `spec.declarative.runtime` (default Python), same as regular agents.
- `go/api/v1alpha2/agent_spec_validation.go` — in `ValidateSubstrateSandboxAgentSpec`: **remove** the BYO rejection (lines 27-29) and the non-Go-runtime rejection (lines 33-38). **Keep** the skills rejection (lines 30-32). Delete the now-unused `substrateSandboxBYOUnsupportedMsg` / `substrateSandboxPythonRuntimeUnsupportedMsg` consts.
- `go/api/v1alpha2/sandboxagent_types.go:42` — **remove** the `XValidation` rule that rejects `type: BYO` on substrate. Keep the skills rule (line 40) and the `substrate`-only rule (line 41).
- Regenerate CRDs + deepcopy: `make -C go generate` and the manifests target; commit the regenerated CRD YAML under `helm/kagent-crds/`.
- Update `go/api/v1alpha2/agent_runtime_test.go` (the "SandboxAgent on substrate uses Go" case → now honors runtime) and `agent_spec_validation_test.go` (BYO + Python now allowed; skills still rejected).

---

## Phase 4 — Substrate ActorTemplate: runtime/type-aware command + image

The substrate SandboxAgent path builds the ActorTemplate in `go/core/pkg/sandboxbackend/substrate/agent_lifecycle.go`. It currently hardcodes the Go command and ignores the pod template's container command.

- `buildSubstrateKagentContainerCommand` / `buildSubstrateGoKagentCommand` (lines 82-109): make the command **depend on runtime + type** (via `v1alpha2.EffectiveDeclarativeRuntimeForAgent(sa)` and `sa.Spec.Type`):
  - **Go declarative:** `/app --host 0.0.0.0 --port 80` (unchanged).
  - **Python declarative:** explicit absolute command, e.g. `/.kagent/.venv/bin/kagent-adk run --host 0.0.0.0 --port 80` (substrate has **no image-entrypoint fallback**, so the command must be explicit and match the distroless entrypoint from Phase 1). Reuse `kagentAgentSecretEnv` (runtime-agnostic) + the `MaterializeFromEnv` support added in Phase 2.
  - **BYO (Go/Python):** use the container's **explicit `Command`/`Args`** from the incoming pod template (`kagentContainer.Command`/`Args`). Because substrate can't fall back to the image entrypoint, **require BYO-on-substrate to set an explicit cmd/args** — add a focused validation error if a BYO substrate agent has no command. (Decision to confirm during implementation: whether to also support reading the image entrypoint via a build-time inspection — default is to require explicit cmd.)
- Image selection: ensure the pod template handed to `BuildSandbox` already carries the correct image. For declarative, extend `resolvePythonRuntimeImage` to take a `full bool` and update `resolveInlineDeployment` (`deployments.go:185`) so Python (not just Go) selects the **full** image when `needsSRTSettings(agent, spec.Sandbox)` is true; otherwise the distroless slim image. For BYO, the user image flows through `resolveByoDeployment` (`deployments.go:264-337`) unchanged.
- Verify the translator `BuildSandbox` path (`manifest_builder.go:552-561`, `substrate/agents_backend.go:37-56`) produces a pod template for **BYO** sandbox agents (today BYO is rejected before reaching here). Confirm `compiler.go` routes `AgentType_BYO` to `resolveByoDeployment` for sandbox/substrate mode and that the resulting container is found by `findKagentContainer`.
- **Listen port / routing:** declarative substrate agents listen on port 80; BYO images commonly serve 8080. Confirm how atenet-router targets the actor port and make the ActorTemplate/routing use the agent's actual port (parameterize rather than hardcode 80 for BYO). Trace `substrate.go` routing + `agent_actor.go`.

---

## Phase 5 — Tests & verification

**Unit (Go):**
- `agent_runtime_test.go`, `agent_spec_validation_test.go` — substrate now allows Python + BYO; skills still rejected; BYO-without-command rejected.
- `substrate/agent_lifecycle_test.go` — assert correct command per (runtime, type): Go `/app …`, Python `…/kagent-adk run …`, BYO uses provided cmd/args.
- `deployments_test.go` — Python slim vs full image selection via `needsSRTSettings`.

**Unit (Python):** materialization tests (Phase 2).

**E2E / manual (`make -C go e2e`, and the `examples/substrate-openclaw` setup):**
- Deploy a substrate `SandboxAgent` with `spec.declarative.runtime: python` → reaches Ready, golden snapshot succeeds, A2A round-trip works.
- Deploy a substrate BYO Go agent and a BYO Python agent (explicit cmd serving A2A on its port) → Ready + A2A round-trip.
- Regression: existing Go declarative substrate agent still works.
- Image: build slim + full Python images; confirm the slim image has no shell/pkg-manager (`docker run … sh` fails) and a normal agent runs; confirm a code-execution agent runs on the full image.

**Lint/generate:** `make -C go lint`, `make -C go generate`, ensure regenerated CRDs committed.

---

## Manual test setup

Substrate e2e cannot be fully exercised by unit tests — it needs a kind cluster with the substrate (ate.dev) control plane installed. This section is the repeatable manual harness used to validate each runtime/type combination.

### 0. Prerequisites
- `make create-kind-cluster` (creates the kind cluster + local `kind-registry:5000`).
- A model config + provider API key Secret in the `kagent` namespace (e.g. `default-model-config`). Declarative agents need this; BYO agents that call an LLM bring their own.
- GCS access for snapshots, OR rely on the controller default `gs://ate-snapshots/<ns>/<name>` location if the cluster has workload-identity/credentials; otherwise set `spec.substrate.snapshotsConfig.location` to a writable bucket. **Note:** if no GCS is reachable in the local environment, golden-snapshot creation will not complete — validation then stops at "ActorTemplate created with correct image/command" (verifiable via `kubectl get actortemplate -o yaml`) rather than a live A2A round-trip. Record which level was reached.

### 1. Install substrate + kagent (from `examples/substrate-openclaw/README.md`)
```bash
cat > substrate-values.yaml <<'EOF'
atelet:
  extraArgs:
    - --localhost-registry-replacement=kind-registry:5000
EOF

export ATEOM_VERSION=v0.0.6
helm upgrade --install substrate-crds oci://ghcr.io/kagent-dev/substrate/helm/substrate-crds
helm upgrade --install substrate oci://ghcr.io/kagent-dev/substrate/helm/substrate \
  --namespace ate-system --create-namespace -f substrate-values.yaml

make helm-install KAGENT_HELM_EXTRA_ARGS="\
  --set controller.substrate.enabled=true \
  --set controller.substrate.ateApiEndpoint=dns:///api.ate-system.svc:443 \
  --set controller.substrate.ateApiInsecure=true \
  --set substrateWorkerPool.create=true \
  --set substrateWorkerPool.ateomImage=ghcr.io/kagent-dev/substrate/ateom-gvisor:${ATEOM_VERSION}"
```

### 2. Build + load the new images into kind
The controller resolves agent images by **link-time digest**, so after building images they must be pushed to `kind-registry:5000` and the controller rebuilt/redeployed so the embedded digests point at them.
```bash
# Build slim + full python images and the go images, push to the kind registry
make build/push DOCKER_REGISTRY=localhost:5000   # adjust to repo's actual target names
# Rebuild + reinstall the controller so digest ldflags pick up the pushed images
make helm-install ...
```
Confirm the slim image is truly distroless:
```bash
docker run --rm --entrypoint sh <python-slim-image> -c 'echo hi'   # must FAIL (no shell)
```

### 3. Test manifests (apply in the `kagent` namespace)

**(a) Declarative Python on substrate** — exercises Phase 2/3/4 + slim image:
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
    systemMessage: "You are a helpful test agent."
  substrate:
    workerPoolRef: { name: kagent-default }
```

**(b) BYO Go on substrate** — exercises Phase 3/4 + BYO path:
```yaml
apiVersion: kagent.dev/v1alpha2
kind: SandboxAgent
metadata: { name: byo-go-substrate, namespace: kagent }
spec:
  type: BYO
  platform: substrate
  byo:
    deployment:
      image: <byo-go-a2a-image>     # serves A2A on :8080
      cmd: "/app"                    # explicit cmd REQUIRED (substrate has no entrypoint fallback)
      args: ["--host","0.0.0.0","--port","8080"]
  substrate:
    workerPoolRef: { name: kagent-default }
```

**(c) BYO Python on substrate** — same as (b) with a python A2A image + explicit `cmd`/`args`.

### 4. Validation checklist (per agent)
1. `kubectl get sandboxagent <name> -o yaml` → Accepted condition true (validation no longer rejects).
2. `kubectl get actortemplate <name> -o yaml` → assert:
   - `containers[0].image` is the expected digest-pinned image (slim python / go / BYO image).
   - `containers[0].command` matches the runtime/type: Go `/app …`, Python `…/kagent-adk static --filepath /config …`, BYO the provided cmd/args.
   - secret-backed env (`KAGENT_CONFIG_JSON`, etc.) present for declarative.
3. Wait for golden snapshot → ActorTemplate `Phase: Ready` (requires reachable snapshot storage).
4. Actor created + Ready; A2A round-trip via the kagent UI/proxy (port-forward `svc/kagent-ui`).
5. Regression: an existing **Go declarative** substrate agent still reaches Ready + responds.

### 5. Image-only validation (no cluster needed)
- `docker build -f python/Dockerfile .` → slim image; run a normal declarative agent locally (`kagent-adk static --filepath ...`) against materialized config.
- `docker build -f python/Dockerfile.full .` → full image; confirm a code-execution/bash-tool agent works (bubblewrap/SRT present).

> Update this section if cluster/image steps differ in practice (registry names, helm flags, snapshot reachability).

---

## Critical files (by phase)

| Area | File |
|------|------|
| Python image | `python/Dockerfile`, `python/Dockerfile.full` (new), `Makefile`, `scripts/controller-digest-ldflags.sh` |
| Python ADK config | `python/packages/kagent-adk/src/kagent/adk/cli.py` (+ new materialize module), mirror `go/adk/pkg/config/config_materialize.go` |
| API constraints | `go/api/v1alpha2/agent_types.go:286-294`, `agent_spec_validation.go`, `sandboxagent_types.go:40-42` |
| Image resolution | `go/core/internal/controller/translator/agent/deployments.go:126-199`, `adk_api_translator.go:121-132` |
| Substrate template | `go/core/pkg/sandboxbackend/substrate/agent_lifecycle.go:82-109`, `agents_backend.go`, `manifest_builder.go:552-561` |
| Routing/port | `go/core/internal/httpserver/handlers/substrate.go`, `substrate/agent_actor.go` |
| CRDs (generated) | `helm/kagent-crds/**` (regenerate) |

## Open items to resolve during implementation
- ~~Encoding used by Go `MaterializeFromEnv`~~ → **Resolved: raw** (env value written verbatim to the file; see `go/adk/pkg/config/config_materialize.go`). Files: `config.json`, `agent-card.json`, `srt-settings.json` in the config dir (`/config`), plus `KAGENT_TOKEN` → `/var/run/secrets/tokens/kagent-token`.
- ~~BYO-on-substrate command~~ → **Resolved: require explicit cmd.** `ValidateSubstrateSandboxAgentSpec` now rejects BYO without `spec.byo.deployment.cmd`; the substrate command uses the container `Command`+`Args` verbatim.
- ~~Actor listen port for BYO~~ → **Resolved: substrate convention is port 80.** atenet-router reaches actors via Host header on port 80 (Go declarative listens on 80; AgentHarness ActorTemplate declares `containerPort: 80`). **BYO images on substrate must serve A2A on :80** (not the usual 8080). Documented; not enforceable from cmd/args. A future improvement is making atenet-router routing port-aware.

## Implementation notes (deviations from original plan)
- **Phase 3 done.** `EffectiveDeclarativeRuntimeForAgent` now simply delegates to `EffectiveDeclarativeRuntime` (no substrate branch). `ValidateSubstrateSandboxAgentSpec` keeps the skills rejection, drops the BYO/Python rejections, and adds a BYO-missing-command check (`substrateSandboxBYOMissingCommandMsg`). Removed the `type:BYO` XValidation rule from `sandboxagent_types.go`; regenerated CRDs and synced to `helm/kagent-crds/templates/`.
- **Phase 4 done (Go side).** `buildSubstrateGoKagentCommand` → `buildSubstrateDeclarativeCommand(runtime)` (Go: `/app …`; Python: `/.kagent/.venv/bin/kagent-adk static …`, port 80). `buildSubstrateKagentContainerCommand(sa, container)` now branches on type: BYO uses the container cmd/args verbatim; declarative prepends secret-backed env. Added `defaultPythonEntrypoint` constant (coupled to the image venv path `/.kagent/.venv`).
- **Image selection change with broader impact:** `needsSRTSettings` is no longer gated to Go. Python declarative agents that need SRT (skills, `executeCodeBlocks`, or BYO-with-sandbox) now resolve the **full** Python image (`PythonADKFullImageDigest`) instead of the single image. Updated golden files `agent_with_code.json`, `agent_with_git_skills.json`, `agent_with_skills.json` (image-only change slim→full) and the test `TestMain` to set `PythonADKFullImageDigest`.
- **Phase 2 done (Python side).** Added `python/packages/kagent-adk/src/kagent/adk/_config_materialize.py` (`materialize_from_env`, raw value → file, 0600, no-op when unset) mirroring the Go `MaterializeFromEnv`. Wired into the `static` command in `cli.py` before config load. Tests in `tests/unittests/test_config_materialize.py` (4 cases) pass. Note: the substrate Python command uses `static` (reads `--filepath`, default `/config`), not `run`.
- **Phase 1 done (Docker, locally build-validated).** Split into:
  - `python/Dockerfile` → **distroless slim** (`gcr.io/distroless/cc-debian12:nonroot`). Builder = `debian:12-slim` + uv standalone Python + `uv sync --no-editable` (self-contained venv). Drops SRT, nodejs, bash sandbox venv. Validated: builds, no shell, full import incl. numpy, CLI runs, `static --filepath /config`.
  - `python/Dockerfile.full` → **full** on `node:20-bookworm-slim` (pinnable, Node 20) with SRT (bubblewrap/socat/ripgrep) + sandbox-runtime + bash venv. Validated: builds, CLI runs, bwrap/node/rg/socat/bash present.
  - `python/Dockerfile.app` → base selected by **tag** (`KAGENT_ADK_VERSION=<v>` slim or `<v>-full`); overrides entrypoint to `static`.
  - Makefile: added `APP_FULL_IMG`/`KAGENT_ADK_FULL_IMG` vars + `build-kagent-adk-full`/`build-app-full` targets; `build`/`build-controller`/`build-img-versions` updated; `build-controller` passes `APP_FULL_IMG` to the ldflags script (which now emits `PythonADKFullImageDigest`).
  - **Deviations found via build validation:**
    1. distroless/cc lacks `libz`/`libbz2`/`liblzma`/`libffi`/`libsqlite3` that standalone CPython + numpy need → copied from builder into `/usr/lib/kagent-libs` on `LD_LIBRARY_PATH`.
    2. distroless nonroot can't `mkdir /config` → pre-create `/config` owned by `65532` in the image (substrate materializes config there; the Deployment path overlays a volume).
    3. debian git lacks `git clone --revision` → use `git init` + `git fetch <sha>` + checkout.
    4. debian's nodejs is v18 but sandbox-runtime needs ≥20 → full image based on `node:20-bookworm-slim`.
    5. sandbox-runtime `prepare` script calls `husky` (absent) → `npm pkg delete scripts.prepare` + `--ignore-scripts`.
  - **End-to-end functional check (no cluster):** ran the slim image with substrate-style env-injected `KAGENT_CONFIG_JSON`/`KAGENT_AGENT_CARD_JSON`; `materialize_from_env` wrote them to `/config` and `kagent-adk static` started uvicorn cleanly. (A2A route paths return 404 in this bare harness — pre-existing ADK base-path behavior, not part of this change.)

## Validated live (2026-06-17) — previously open items now resolved
- Golden-snapshot creation + actor reachability: PASS for Python-declarative, Go-declarative, BYO (see Live validation + Phase 4 bug sections).
- `/config` pre-create + `LD_LIBRARY_PATH` under runsc/gVisor: `/config` works; `LD_LIBRARY_PATH` does **not** survive from the image `ENV` (substrate drops it) — now re-supplied via the ActorTemplate (`pythonRuntimeImageEnv`). Python actor restore + A2A round-trip confirmed.
- BYO serves on :80 (atenet-router convention): confirmed.

## Still open
- Sample/test agents that `FROM kagent-adk` and rely on code execution must switch to the `-full` tag (see Follow-up).

## UI changes (DONE)
Updated `ui/` so the feature is usable from the agent form:
- `ui/src/lib/sandboxAgentForm.ts`: `substrateSupportedForAgentType` now allows BYO (and Declarative); `defaultDeclarativeRuntimeForSandboxPlatform` keeps Go as the substrate default but it is no longer a restriction (comment updated).
- `ui/src/app/agents/new/page.tsx`: runtime selector now shown on substrate (`showDeclarativeRuntimeField` no longer excludes substrate); edit-load honors the persisted runtime instead of forcing Go; removed the unused `isSubstrateSandbox` edit-load var; switch-to-substrate uses the default-runtime helper (still selectable) and keeps clearing skills (still unsupported); copy updated to "declarative (Python or Go) and BYO … BYO images must set an explicit command and serve A2A on port 80. Skills are not supported on substrate yet."
- Added client-side validation: BYO on substrate requires a command (`byoCmd` error) — `agent-form-types.ts` (+`byoCmd`), `ByoDeploymentFields.tsx` (render error near the Command field), `page.tsx` `validateForm`.
- Tests `ui/src/lib/__tests__/sandboxAgentForm.test.ts` updated (BYO now supported). Full UI suite: **264 tests pass**; eslint clean on changed files; changed source files typecheck clean (the `toBe` tsc errors are a pre-existing jest-types/root-tsconfig quirk affecting untouched test files too).
- Skills remain unsupported on substrate (backend still rejects) — UI skills gating left intact.

## Live validation (executed 2026-06-17, then torn down)
Ran a clean kind `kagent` cluster + substrate v0.0.6 + `make helm-install` and applied the agents.
Results (full runbook + real outputs in `docs/substrate-python-byo-manual-validation.md`):
- CRD admission accepts Declarative **Python**, Declarative **Go**, **BYO Go**, **BYO Python** on substrate.
- BYO without `cmd` → `Accepted=False` with the expected message.
- ActorTemplate generated with the correct image + verbatim/declarative command + config env per runtime/type.
- **Python declarative, Go declarative, AND BYO all reached `Ready=True` end-to-end** (golden snapshot via bundled `rustfs` — no external GCS — + actor + workload). BYO was validated with the repo's `python/samples/langgraph/kebab` sample (cmd `/app/.venv/bin/python samples/langgraph/kebab/kebab/cli.py`, env `PORT=80`+`KAGENT_URL`+`OPENAI_API_KEY`, image referenced via `localhost:5001/...`, WorkerPool replicas=3). Full recipe + gotchas in the runbook.

## Phase 4 bug found & fixed during chat validation (2026-06-17): substrate drops image `ENV`
Live chat against the **Python declarative** agent initially failed — the actor would not restore: worker logs showed gVisor `FATAL ERROR: ... inconsistent private memory files on restore: savedMFOwners = [pause:/]`. That error is a *symptom*. Root cause, found in the **golden actor's** logs: the Python process crashed on startup with `ImportError: libz.so.1: cannot open shared object file` (numpy C-extension), because **substrate builds the OCI `Process.Env` from a hardcoded `PATH` + the ActorTemplate env only and ignores the image's `ENV` directives** (`cmd/atelet/oci.go` `prepareOCIDirectory` — same class as "no image-entrypoint fallback"). The slim Python image relies on `ENV LD_LIBRARY_PATH=/usr/lib/kagent-libs`, which substrate dropped, so numpy couldn't load `libz.so.1`. The crashed `kagent` container left only `pause` memory-resident → golden snapshot captured `pause` only → template went `Ready` (misleading) → every restore failed the MemoryFile consistency check.

**Fix:** `buildSubstrateKagentContainerCommand` now re-supplies the Python runtime image's runtime-critical ENV via the ActorTemplate for declarative Python (`pythonRuntimeImageEnv`: `LD_LIBRARY_PATH`, `VIRTUAL_ENV`, `PYTHONUNBUFFERED`, `LANG`, `LC_ALL`). Go declarative (static binary) needs none; BYO is unchanged (BYO images must pass any required env via `spec.byo.deployment.env`, since substrate drops their baked-in `ENV` too). Unit tests updated: `TestBuildSubstrateKagentContainerCommandDeclarative` and `TestBuildSandboxAgentActorTemplate` now assert Python carries `LD_LIBRARY_PATH` and Go/BYO do not.

**Re-validated live (controller rebuilt as `controller:libfix1`, agent recreated):** golden Python started cleanly (`Uvicorn running on http://0.0.0.0:80`), snapshot captured a healthy container, and an A2A `message/send` (with a `contextId`) **restored an actor and ran the agent end-to-end** — reached the LLM call (only OpenAI `429 insufficient_quota` blocked a final answer). Zero `inconsistent private memory` errors post-fix. (Note: the libfix1 tag was a validation build; the change is in-tree and folds into `make build-controller`.)

> **New substrate contract documented:** substrate does not apply image `ENV` (only its hardcoded `PATH` + ActorTemplate env), in addition to not falling back to the image entrypoint. This refines the earlier "no NEW contracts" conclusion — the contract pre-existed in substrate, but our Python implementation had to account for it.

## Config-change propagation (config-hashed ActorTemplate + session actors), 2026-06-17
**Problem found in validation:** editing a SandboxAgent's `modelConfig` (e.g. OpenAI → Gemini) updated the config Secret but chat kept using the old model. Two causes: (1) a golden snapshot is an immutable memory image and substrate's `actortemplate_controller` snapshots **once** then no-ops in `PhaseReady` — updating the same-named ActorTemplate never re-snapshots; (2) per-session actors are created once from the golden and only *resumed* afterward (`EnsureSessionActor`), so they keep the old config. Sessions themselves are safe — `KAgentSessionService` persists history in the kagent DB (Postgres), so this is equivalent to rolling a Deployment (which the config-hash pod annotation already does for the non-substrate path); only ephemeral in-actor state resets.

**Design (content-addressed identities + blue-green):**
- ActorTemplate name = `<base>-<shortConfigHash>`; the hash is the translator's `kagent.dev/config-hash` (read off the pod template, stamped as an annotation). A config change ⇒ new template name ⇒ fresh golden.
- Consumers (`ComputeReady`, chat `EnsureSessionActor`/suspend/delete) resolve the live template via `ResolveCurrentActorTemplate`, which returns the **newest *Ready*** template (fallback newest only on first build). This is the blue-green pivot: during a rebuild it keeps returning the previous Ready template, so chat and readiness stay on the working golden and flip atomically once the new is Ready. The chat actor backend gained a kube client for this.
- Session actor id folds in the hash (`SandboxAgentSessionActorID(sa, configHash, sessionID)`), keeping the `asr-<ns>-<name>-` prefix. A config change ⇒ new id ⇒ `GetActor` miss ⇒ `CreateActor` from the new golden. The hash is in the id (not just compared) so the synchronous chat path never has to drive substrate's multi-step async `deleteActor` (suspend→poll→delete) inline.
- **Orphaned session-actor reaping on rollout (2026-06-29).** When `EnsureSessionActor` *creates* a new session actor (the `GetActor` miss above — i.e. a config rollout has moved sessions onto a new golden), it fires `reapOrphanedSessionActors` on a detached, time-bounded goroutine (mirrors the transport's post-response suspend scheduling). The reaper **sweeps all of the agent's session actors** (via `ListActors`) and deletes every one that is **under a superseded ActorTemplate** (any template other than the current desired one, resolved by the shared `selectCurrentActorTemplate`) **and already SUSPENDED** (`deleteActorIfSuspended`, a single GetActor+DeleteActor; no suspend→poll→delete loop). So when *any* session resumes after a rollout, *all* sessions' stale actors are cleaned, not just the resuming one. Safety: golden actors (UUID ids vs. the `asr-` session prefix) are never reaped — they are the retained snapshots; actors under the current template (live sessions) and the just-created actor are kept; a still-RUNNING orphan (in-flight pre-rollout request) is left to quiesce and reaped on a later pass; and if the current template can't be resolved it reaps nothing. This keeps stale session actors from accumulating across rollouts without a controller-side reaper (the controller can't enumerate sessions), and never adds latency to or fails the chat request. Previously these orphans lived until the session or SandboxAgent was deleted. (This is essentially the earlier `ReapStaleSessionActors`, now triggered on actor creation rather than from the reconcile loop.) **Idempotent, not single-shot:** ate-api's `ListActors` is eventually-consistent, so one sweep reaps only what that pass sees; stragglers are caught on the next session create. Live validation (2026-06-29) confirmed this — sweeping orphans across multiple sessions took two creates to fully converge (one orphan was absent from the first sweep's `ListActors` snapshot), with goldens and current-template actors left intact throughout.
- **Blue-green template lifecycle (corrects an earlier downtime bug).** ActorTemplate is excluded from the reconciler's generic prune (substrate `OwnedResourceTypesFor` → empty; `RoutingBackend.OwnedResourceTypesFor` now delegates to the platform's per-agent method, not `GetOwnedResourceTypes`; ActorTemplate stays in `GetOwnedResourceTypes` for watches). The *first* implementation let the generic prune delete the old template in the same reconcile that created the new one → a "no Ready template" gap → **downtime** (and, when a still-building golden was orphaned, a permanently-RUNNING golden that can't be suspended → worker leak). Now `Lifecycle.RetireSupersededTemplates` retires an old template only **after** a newer one is Ready, and only when the old template's golden is itself **Suspended** (`Phase==Ready`) — deleting the golden before the template object. We never orphan a RUNNING golden, so the leak is structurally impossible. Owner-ref GC still removes all templates on SandboxAgent deletion.
- **Why no per-request signal is needed.** Substrate exposes only `Actor_Status` RUNNING/SUSPENDED (worker "busy" = an actor is *resumed*, not "a request is in flight"; no draining/inflight anywhere in ate-api/atenet-router). kagent's transport owns the per-request lifecycle — it resumes a session actor before proxying and suspends it after the response body closes. So cutover never force-suspends: stale session actors are reaped only when **SUSPENDED** (`ReapStaleSessionActors` via `deleteActorIfSuspended`; RUNNING ones are skipped and converge via the transport).

**Live validation (controller `controller:bluegreen1`, single-replica WorkerPool):** baseline gemini Ready → flip `modelConfig` Gemini→OpenAI. During the rebuild (~5s, ~15s) **both** templates present — old gemini `16cf0ceb4f243467` stayed **Ready** while openai `7f52a218751d4f4f` built (`WaitGoldenActor`), and the agent's `Ready` condition stayed **True** the whole time (no gap). At ~25s the openai golden was Ready, the gemini template + its golden were **retired** (deleted, not orphaned), only the openai template remained, and chat switched to **openai**. All on **1 worker** — confirming no extra replica is required to avoid downtime. Unit tests: `config_hash_test.go` (resolver prefers newest-Ready; `RetireSupersededTemplates` keeps newest+active and retires older Ready) and `filter_translator_owned_test.go` (ActorTemplate excluded from substrate prune list).

> **Substrate support confirmed before implementing:** no per-agent/per-template uniqueness (`CreateActor` enforces only actor-id uniqueness); a Ready golden is Suspended and frees its worker, so concurrent old+new templates don't pin two workers; the only ordering rule — don't delete the old template until chat has switched (`CreateActor` requires the template to exist; `ResumeActor`/`DeleteActor` do not) — is honored by retiring only after the new is Ready.

> **Earlier (superseded) attempt:** `controller:cfghash1..3` used immediate-prune + a `ReapStaleActors` reaper that also force-suspended goldens. It hit the downtime gap and a worker leak (orphaned RUNNING goldens from rapid flips, un-suspendable once their template was gone). Replaced by the blue-green design above; the pre-existing leaked goldens were cleared with `kubectl rollout restart deploy/kagent-default-deployment`.

## Follow-up (downstream impact of the image split)
`go/core/test/e2e/agents/kebab/Dockerfile` does `FROM kagent-adk` + `RUN uv sync`, which **breaks on the new distroless slim base** (no shell/uv at runtime), so `make push-test-agent` now fails on that agent. Fix before merge: repoint that Dockerfile at the **full** Python image (`kagent-adk:<version>-full`) or restructure it to not need uv at build. (The python `samples/*` Dockerfiles are unaffected — they build from `ghcr.io/astral-sh/uv` directly.)

## E2E / test coverage note
There are **no e2e tests for substrate** (nothing under `go/core/test/e2e` references it); substrate is covered by unit/integration tests. Added side-by-side coverage in `go/core/pkg/sandboxbackend/substrate/agent_lifecycle_test.go`:
- `TestBuildSubstrateDeclarativeCommand` and `TestBuildSubstrateKagentContainerCommandDeclarative` (Go + Python), `TestBuildSubstrateKagentContainerCommandBYO`.
- `TestBuildSandboxAgentActorTemplate` — builds the full ActorTemplate for **Go declarative, Python declarative, and BYO** and asserts the pinned image, the explicit command, and config-env wiring (declarative carries `KAGENT_CONFIG_JSON`; BYO does not).
