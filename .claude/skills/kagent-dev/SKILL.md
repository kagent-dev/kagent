---
name: kagent-dev
description: Comprehensive guide for kagent development covering CRD modifications, E2E testing, PR workflows, local deployment, and debugging. Use this skill when working on the kagent codebase itself - adding features, fixing bugs, reviewing PRs, running tests, or troubleshooting failures. Trigger for any kagent development tasks including understanding the codebase structure, modifying CRDs, writing tests, deploying locally, or analyzing CI failures.
---

# Kagent Development Guide

## Quick Reference

### Most Common Commands

```bash
# Local Kind cluster setup
make create-kind-cluster
make helm-install  # Builds images and deploys to Kind

# Code generation (after CRD type changes)
make controller-manifests  # generate + copy CRDs to helm (recommended)
make -C go generate         # DeepCopy methods only

# Build & test
make -C go test               # Unit tests (includes golden file checks)
make -C go e2e                # E2E tests (needs KAGENT_URL)
make -C go lint               # Go lint
make -C python lint           # Python lint

# Golden file regeneration (after translator changes)
UPDATE_GOLDEN=true make -C go test

# Set KAGENT_URL for E2E tests
export KAGENT_URL="http://$(kubectl get svc -n kagent kagent-controller -o jsonpath='{.status.loadBalancer.ingress[0].ip}'):8083"

# Check cluster status
kubectl get agents.kagent.dev -n kagent
kubectl get pods -n kagent
```

### Repository Structure

```
kagent/
├── go/                      # Go workspace (go.work: api, core, adk)
│   ├── api/                 # Shared types module
│   │   ├── v1alpha2/        # Current CRD types (agent_types.go, etc.)
│   │   ├── adk/             # ADK config types (types.go) — flows to Python runtime
│   │   ├── database/        # GORM models
│   │   ├── httpapi/         # HTTP API types
│   │   └── config/crd/bases/ # Generated CRD YAML
│   ├── core/                # Infrastructure module
│   │   ├── cmd/             # Controller & CLI binaries
│   │   ├── internal/        # Controllers, HTTP server, DB impl
│   │   │   └── controller/translator/agent/  # Translator files:
│   │   │       ├── adk_api_translator.go     # Main: TranslateAgent(), builds K8s objects
│   │   │       ├── deployments.go            # resolvedDeployment struct, resolve*Deployment()
│   │   │       ├── template.go               # Prompt template resolution
│   │   │       └── testdata/                 # Golden test inputs/ and outputs/
│   │   └── test/e2e/        # E2E tests
│   └── adk/                 # Go Agent Development Kit
│
├── python/                  # Python workspace (UV)
│   ├── packages/            # UV workspace packages (kagent-adk, etc.)
│   └── samples/             # Example agents (adk/, crewai/, langgraph/, openai/)
│
├── helm/                    # Kubernetes deployment
│   ├── kagent-crds/         # CRD chart (install first)
│   └── kagent/              # Main app chart
│
└── ui/                      # Next.js web interface
```

**Module Boundaries:**
- **go/api/** - Shared types used by both core and adk. Import from other modules OK.
- **go/core/** - Infrastructure code. Should NOT import from go/adk.
- **go/adk/** - Agent runtime. Can import from go/api, not from go/core.

---

## Adding CRD Fields

Adding a field propagates from the type definition through codegen, translator, and tests.

### Step-by-Step Workflow

0. **Check if the field already exists**

   Before writing any code, search existing types — many common fields are already implemented. The Agent CRD already has fields for image, resources, env, replicas, imagePullPolicy, tolerations, service accounts, volumes across `SharedDeploymentSpec`, `DeclarativeAgentSpec`, and related structs.

   ```bash
   grep -rn "fieldName\|FieldName" go/api/v1alpha2/
   ```

1. **Edit the CRD type definition**

   File: `go/api/v1alpha2/agent_types.go` (or the relevant CRD type file)

   Choose the right struct:
   - `DeclarativeAgentSpec` — agent behavior (system message, model, tools, runtime config)
   - `SharedDeploymentSpec` — deployment concerns shared by Declarative + BYO (image, resources, env, replicas, imagePullPolicy)
   - `DeclarativeDeploymentSpec` / `ByoDeploymentSpec` — type-specific deployment config

   ```go
   // NewField is a description of what this field does
   // +optional
   // +kubebuilder:validation:Enum=value1;value2;value3
   NewField *string `json:"newField,omitempty"`
   ```

   Use pointers for optional primitives (to distinguish "unset" from zero value), value types for slices/maps.

2. **Run code generation**

   ```bash
   make controller-manifests
   ```

   This runs `make -C go generate` (DeepCopy methods + CRD YAML) and copies CRD YAML to `helm/kagent-crds/templates/`. Run steps separately if needed.

3. **Update the translator (if field affects K8s resources)**

   Two key files depending on what your field does:

   - **`deployments.go`** — for fields affecting the Deployment spec (image, resources, env, volumes, replicas). Add to `resolvedDeployment` struct, wire in `resolveInlineDeployment()` / `resolveByoDeployment()`.

   - **`adk_api_translator.go`** — for fields affecting ADK config JSON, Service, or overall translation. Main method: `TranslateAgent()`.

   Pattern (check-if-set, else use default):
   ```go
   if spec.Declarative.NewField != nil {
       // use *spec.Declarative.NewField
   }
   ```

   See `references/translator-guide.md` for detailed patterns.

4. **If the field flows to the Python runtime**

   Some fields need to reach the Python agent process (e.g., `stream`, `executeCodeBlocks`):
   - `go/api/adk/types.go` — add field to `AgentConfig`
   - `python/packages/kagent-adk/src/kagent/adk/types.py` — add corresponding field

5. **Regenerate golden files**

   ```bash
   UPDATE_GOLDEN=true make -C go test
   git diff go/core/internal/controller/translator/agent/testdata/outputs/
   ```

   Review the diff — only your expected changes should appear. If unexpected changes show up, fix the translator logic rather than committing bad golden files.

6. **Add E2E test**

   File: `go/core/test/e2e/invoke_api_test.go`

   Tests follow: create resources → wait for Ready → send A2A messages → verify → clean up. Look at existing tests for patterns.

7. **Run tests**

   ```bash
   make -C go test    # Unit tests + golden file checks
   make -C go lint    # Lint
   make -C go e2e     # E2E (needs Kind cluster + KAGENT_URL)
   ```

### Common Issues

**Golden files mismatch:** `UPDATE_GOLDEN=true make -C go test`, review diff, commit if intentional.

**CRD validation errors:** Check kubebuilder markers and JSON tags (camelCase). Ensure required fields have values in test fixtures.

**Field not in created resources:** Check translator is using the field. Verify correct struct path (e.g., `spec.Declarative.Deployment.ImagePullPolicy` not `spec.Declarative.ImagePullPolicy`).

For detailed examples of different field types and validation markers, see `references/crd-workflow-detailed.md`.

---

## E2E Testing

### Quick Diagnosis

```
"connection refused"        → KAGENT_URL wrong (check LoadBalancer IP + port 8083)
"context deadline exceeded" → Timeout (pod status, controller logs, mock LLM reachability)
"unexpected field"          → CRD mismatch (run make controller-manifests, redeploy)
"failed to create agent"    → Validation error (check kubebuilder markers, required fields)
```

### Environment Variables

**KAGENT_URL (required):**
```bash
export KAGENT_URL="http://$(kubectl get svc -n kagent kagent-controller -o jsonpath='{.status.loadBalancer.ingress[0].ip}'):8083"
curl -v $KAGENT_URL/healthz  # Verify reachable
```

Common mistakes: using ClusterIP, using localhost, forgetting `:8083`, not setting it at all.

**KAGENT_LOCAL_HOST (usually auto-detected):**
Host IP for mock LLM server reachability from Kind pods:
- macOS: `host.docker.internal` (auto-detected)
- Linux: `172.17.0.1` (auto-detected, set explicitly if needed)

**SKIP_CLEANUP=1:** Preserve resources after test failure for debugging.

### Prerequisites

```bash
make create-kind-cluster
make helm-install
make push-test-agent  # Builds test images (basic-openai, poem-flow, kebab)
```

### Running Tests

```bash
# All tests
cd go/core
KAGENT_URL="..." go test -v github.com/kagent-dev/kagent/go/core/test/e2e -failfast -shuffle=on

# Specific test
KAGENT_URL="..." go test -v github.com/kagent-dev/kagent/go/core/test/e2e -run TestE2EInvokeInlineAgent
```

### Debugging

```bash
kubectl get pods -n kagent                                    # Pod status
kubectl logs -n kagent deployment/kagent-controller           # Controller logs
kubectl get agent <name> -n kagent -o jsonpath='{.status}'    # Agent status
curl -v $KAGENT_URL/healthz                                   # Controller reachable?
```

**Reproducing locally (without cluster):** Follow `go/core/test/e2e/README.md` — extract agent config, start mock LLM server, run agent with `kagent-adk test`. Much faster iteration than full cluster.

**CI-specific:** E2E runs in matrix (`sqlite` + `postgres`). If only one database variant fails, it's likely database-related. If both fail, it's infrastructure. Most common CI-only failure: mock LLM unreachability because `KAGENT_LOCAL_HOST` detection fails on Linux.

See `references/e2e-debugging.md` for comprehensive debugging techniques.

---

## PR Review Workflow

### Impact Checklist

**CRD type changes** → codegen committed, helm CRD manifests updated, translator updated (if field affects resources), E2E test added

**Translator changes** → golden files regenerated and committed, E2E test (if behavior changes)

**Python ADK changes** → sample agents updated (if breaking), version bump in pyproject.toml

### Checking Conventions

- Commit messages: Conventional Commits format (`feat:`, `fix:`, etc.), signed with `-s`
- Error handling: wrap with `fmt.Errorf("context: %w", err)`
- Kubebuilder markers: no Helm template syntax (`{{ }}`) in doc comments

### Testing Changes Locally

```bash
make helm-install    # Rebuild + redeploy with PR code
make -C go test      # Unit tests
make -C go e2e       # E2E tests
```

---

## Local Development

### Quick Iteration Targets

```bash
make build-controller      # Controller image only (fastest for Go changes)
make build-app             # Python agent image only
make build-ui              # UI image only
make helm-install-provider # Redeploy without rebuilding images (helm changes only)
make helm-install          # Full rebuild + redeploy
```

### Debugging

```bash
# Agent won't start
kubectl describe pod -n kagent <pod-name>
kubectl logs -n kagent <pod-name> -c kagent

# Agent not Ready
kubectl get agent <name> -n kagent -o jsonpath='{.status.conditions}' | jq

# Controller errors
kubectl logs -n kagent deployment/kagent-controller | grep <agent-name>

# Template resolution errors
kubectl get configmap -n kagent kagent-builtin-prompts
kubectl get agent <name> -n kagent -o jsonpath='{.status.conditions[?(@.type=="Accepted")]}'
```

---

## CI Troubleshooting

| Failure | Fix |
|---------|-----|
| manifests-check | `make controller-manifests` then commit generated files |
| go-lint depguard | Replace `sort` with `slices`, `io/ioutil` with `io`/`os` (see `go/.golangci.yaml`) |
| golden files mismatch | `UPDATE_GOLDEN=true make -C go test`, review diff, commit |
| test-e2e timeout | Check pod status, KAGENT_URL, mock LLM reachability via KAGENT_LOCAL_HOST |

```bash
gh pr checks <pr-number>                                    # CI status
gh run view <run-id> --job <job-id> --log                   # Job logs
gh run view <run-id> --log | grep -A 50 "fail print info"   # Post-failure diagnostics
```

For comprehensive CI failure patterns, see `references/ci-failures.md`.

---

## Code Patterns

### Commit Messages

`<type>: <description>` — Types: `feat`, `fix`, `docs`, `refactor`, `test`, `chore`, `perf`, `ci`

Always sign: `git commit -s -m "feat: add support for X"`

### Error Handling

```go
if err != nil {
    return fmt.Errorf("failed to create deployment for agent %s: %w", agentName, err)
}
```

### Kubebuilder Markers

```go
// +optional
// +kubebuilder:validation:Enum=value1;value2;value3
// +kubebuilder:validation:MinLength=1
// +kubebuilder:default="value"
```

Don't use Go template syntax (`{{ }}`) in doc comments — Helm will try to parse them.

---

## Additional Resources

- `references/crd-workflow-detailed.md` - Field type examples, complex validation, pointer vs value types
- `references/translator-guide.md` - Translator patterns, `deployments.go` and `adk_api_translator.go`
- `references/e2e-debugging.md` - Comprehensive E2E debugging, local reproduction
- `references/ci-failures.md` - CI failure patterns and fixes
