---
name: kagent-dev
description: Comprehensive guide for kagent development covering CRD modifications, E2E testing, PR workflows, local deployment, and debugging. Use this skill when working on the kagent codebase itself - adding features, fixing bugs, reviewing PRs, running tests, or troubleshooting failures. Trigger for any kagent development tasks including understanding the codebase structure, modifying CRDs, writing tests, deploying locally, or analyzing CI failures.
---

# Kagent Development Guide

This skill helps you work effectively on the kagent codebase. It covers common development workflows, testing patterns, and troubleshooting techniques.

## Quick Reference

### Most Common Commands

```bash
# Local Kind cluster setup
make create-kind-cluster
make helm-install  # Builds images and deploys to Kind

# Code generation (after CRD type changes)
make -C go generate           # DeepCopy methods only
make -C go manifests          # CRD YAML + RBAC (runs generate first)
make controller-manifests     # manifests + copies CRDs to helm (recommended)

# Build & test
make -C go test               # Unit tests (includes golden file checks)
make -C go e2e                # E2E tests (needs KAGENT_URL)
make -C go lint               # Go lint
make -C python lint           # Python lint (ruff format only; CI also runs ruff check)

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
│   │   ├── client/          # REST client SDK
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
│   ├── packages/            # UV workspace packages
│   │   ├── kagent-adk/      # Main ADK
│   │   ├── kagent-core/     # Core utilities
│   │   ├── kagent-skills/   # Skills framework
│   │   ├── kagent-openai/   # OpenAI agent support
│   │   ├── kagent-langgraph/ # LangGraph agent support
│   │   └── kagent-crewai/   # CrewAI agent support
│   └── samples/             # Example agents (adk/, crewai/, langgraph/, openai/)
│
├── helm/                    # Kubernetes deployment
│   ├── kagent-crds/         # CRD chart (install first)
│   ├── kagent/              # Main app chart (values.yaml has defaults)
│   ├── agents/              # Pre-built agent charts
│   └── tools/               # Tool charts (grafana-mcp, querydoc)
│
└── ui/                      # Next.js web interface
```

**Module Boundaries:**
- **go/api/** - Shared types used by both core and adk. Import from other modules OK.
- **go/core/** - Infrastructure code. Should NOT import from go/adk.
- **go/adk/** - Agent runtime. Can import from go/api, not from go/core.

---

## Adding CRD Fields

Adding a field to a CRD involves several connected steps. The field must propagate from the type definition through code generation, the translator, and into tests.

### Step-by-Step Workflow

0. **Check if the field already exists**

   Before writing any code, search the existing types — many common fields are already implemented. The Agent CRD already has fields for image, resources, env, replicas, imagePullPolicy, tolerations, service accounts, volumes, and more across `SharedDeploymentSpec`, `DeclarativeAgentSpec`, and related structs.

   ```bash
   grep -rn "fieldName\|FieldName" go/api/v1alpha2/
   ```

   If the field exists, you can skip straight to using it. If it exists but needs modification (e.g., adding validation), start from step 2.

1. **Edit the CRD type definition**

   File: `go/api/v1alpha2/agent_types.go` (or the relevant CRD type file)

   Choose the right struct for your field:
   - `DeclarativeAgentSpec` — agent behavior (system message, model, tools)
   - `SharedDeploymentSpec` — deployment concerns shared by Declarative + BYO (image, resources, env, replicas, imagePullPolicy)
   - `DeclarativeDeploymentSpec` / `ByoDeploymentSpec` — type-specific deployment config
   - `AgentSpec` — top-level agent metadata (type, description)

   ```go
   // NewField is a description of what this field does
   // +optional
   // +kubebuilder:validation:Enum=value1;value2;value3
   NewField *string `json:"newField,omitempty"`
   ```

   Key points:
   - Add kubebuilder markers for validation
   - Use pointers for optional primitives (to distinguish "unset" from zero), value types for slices/maps
   - Follow patterns of surrounding fields in the same struct

2. **Run code generation**

   ```bash
   # Recommended: does everything in one step
   make controller-manifests
   ```

   This runs:
   - `make -C go generate` → DeepCopy methods (`zz_generated.deepcopy.go`)
   - `make -C go manifests` → CRD YAML + RBAC in `go/api/config/crd/bases/`
   - Copies CRD YAML to `helm/kagent-crds/templates/`

   These are separate make targets you can run individually if needed.

3. **Update the translator (if field affects K8s resources)**

   Two key files depending on what your field does:

   - **`deployments.go`** — for fields affecting the Deployment spec (image, resources, env, volumes, replicas, pull policy). Add to the `resolvedDeployment` struct, wire in `resolveInlineDeployment()` / `resolveByoDeployment()`.

   - **`adk_api_translator.go`** — for fields affecting ADK config JSON, Service, or overall translation. Main method: `TranslateAgent()`.

   Pattern (check-if-set, else-use-default):
   ```go
   imagePullPolicy := corev1.PullPolicy(DefaultImageConfig.PullPolicy)
   if spec.ImagePullPolicy != "" {
       imagePullPolicy = corev1.PullPolicy(spec.ImagePullPolicy)
   }
   ```

   See `references/translator-guide.md` for detailed patterns.

4. **If the field flows to the Python runtime**

   Some fields need to reach the Python agent process (e.g., `stream`, `executeCodeBlocks`). Update both:
   - `go/api/adk/types.go` — add field to `AgentConfig`
   - `python/packages/kagent-adk/src/kagent/adk/types.py` — add corresponding field

5. **Regenerate golden files**

   ```bash
   UPDATE_GOLDEN=true make -C go test
   git diff go/core/internal/controller/translator/agent/testdata/outputs/
   ```

   Review the diff — only your expected changes should appear.

6. **Add E2E test**

   File: `go/core/test/e2e/invoke_api_test.go`

   Look at existing tests for patterns. Tests follow: create resources → wait for Ready → send A2A messages → verify → clean up.

7. **Run tests**

   ```bash
   make -C go test    # Unit tests + golden file checks
   make -C go lint    # Lint
   make -C go e2e     # E2E (needs Kind cluster + KAGENT_URL)
   ```

### Common Issues

**CRD validation errors:** Check kubebuilder markers and JSON tags (camelCase).

**Golden files mismatch:** `UPDATE_GOLDEN=true make -C go test`, review diff, commit if intentional.

**Field not in created resources:** Check translator is using the field. Verify correct struct path (e.g., `spec.Declarative.Deployment.ImagePullPolicy` not `spec.Declarative.ImagePullPolicy`).

For detailed examples of different field types, see `references/crd-workflow-detailed.md`.

---

## E2E Testing

### Environment Variables

**KAGENT_URL (required):**
```bash
export KAGENT_URL="http://$(kubectl get svc -n kagent kagent-controller -o jsonpath='{.status.loadBalancer.ingress[0].ip}'):8083"
```

**KAGENT_LOCAL_HOST (usually auto-detected):**
Host IP for mock LLM server reachability from Kind pods:
- macOS: `host.docker.internal` (auto-detected)
- Linux: `172.17.0.1` (auto-detected, set explicitly if needed)

**SKIP_CLEANUP=1:** Preserve resources after test failure for debugging.

Common KAGENT_URL mistakes: using ClusterIP, using localhost, forgetting `:8083`, not setting it at all.

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

### Quick Diagnosis

```
"connection refused"        → KAGENT_URL wrong (check LoadBalancer IP + port 8083)
"context deadline exceeded" → Timeout (pod status, controller logs, mock LLM reachability)
"unexpected field"          → CRD mismatch (run make controller-manifests, redeploy)
"failed to create agent"    → Validation error (check kubebuilder markers, required fields)
```

```bash
kubectl get pods -n kagent                                    # Pod status
kubectl logs -n kagent deployment/kagent-controller           # Controller logs
kubectl get agent <name> -n kagent -o jsonpath='{.status}'    # Agent status
curl -v $KAGENT_URL/healthz                                   # Controller reachable?
```

### CI-Specific E2E Debugging

CI runs E2E tests in a **matrix** (`sqlite` and `postgres`). If only one database variant fails, it's likely database-related latency. If both fail, it's infrastructure (network/resources).

The most common CI-only failure is **mock LLM server unreachability**: macOS uses `host.docker.internal` (reliable), but Linux CI detects the Kind network gateway IP for `KAGENT_LOCAL_HOST`. If this detection fails, agent pods can't reach the mock LLM and the test hangs.

```bash
# Get CI logs for the failing job
gh run view <run-id> --job <job-id> --log | grep -A 50 "fail print info"

# Key things to look for in CI logs:
# - Pod status: ImagePullBackOff, Pending, CrashLoopBackOff
# - Last test log line before timeout (identifies which phase hung)
# - KAGENT_LOCAL_HOST value (should be a valid IP, not empty)
```

See `references/e2e-debugging.md` for comprehensive techniques including local reproduction.

---

## Golden Files

Translator tests use golden files to verify generated Kubernetes manifests.

**Location:** `go/core/internal/controller/translator/agent/testdata/outputs/`

```bash
# Regenerate after translator changes
UPDATE_GOLDEN=true make -C go test

# Review before committing (only expected changes should appear)
git diff go/core/internal/controller/translator/agent/testdata/outputs/

# Commit alongside translator change
git add go/core/internal/controller/translator/agent/testdata/outputs/
git commit -s -m "test: regenerate golden files after <change>"
```

If unexpected changes appear, fix the translator logic rather than committing bad golden files.

---

## PR Review Workflow

### Impact Checklist

**CRD type changes** → codegen, helm CRD manifests, translator update (if affects resources), E2E test

**Translator changes** → updated golden files, E2E test (if behavior changes)

**Python ADK changes** → sample agents updated (if breaking), version bump in pyproject.toml

### Testing

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
```

---

## CI Troubleshooting

| Failure | Fix |
|---------|-----|
| manifests-check | `make controller-manifests` then commit generated files |
| go-lint depguard | Replace `sort` with `slices`, `io/ioutil` with `io`/`os` (see `go/.golangci.yaml`) |
| golden files mismatch | `UPDATE_GOLDEN=true make -C go test`, review diff, commit |
| test-e2e timeout | Check pod status, KAGENT_URL, mock LLM reachability via KAGENT_LOCAL_HOST |

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
