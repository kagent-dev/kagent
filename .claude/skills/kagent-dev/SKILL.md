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

# Build & test
make -C go generate           # After CRD changes
make -C go test               # Unit tests
make -C go e2e                # E2E tests (needs KAGENT_URL)

# Lint
make -C go lint
make -C python lint

# Set KAGENT_URL for E2E tests
export KAGENT_URL="http://$(kubectl get svc -n kagent kagent-controller -o jsonpath='{.status.loadBalancer.ingress[0].ip}'):8083"

# Check cluster status
kubectl get agents.kagent.dev -n kagent
kubectl get pods -n kagent
```

### Repository Structure

```
kagent/
├── go/                      # Go workspace (go.work)
│   ├── api/                 # Shared types module
│   │   ├── v1alpha2/        # Current CRD types
│   │   ├── database/        # GORM models
│   │   ├── httpapi/         # HTTP API types
│   │   └── client/          # REST client SDK
│   ├── core/                # Infrastructure module
│   │   ├── cmd/             # Controller & CLI binaries
│   │   ├── internal/        # Controllers, HTTP server, DB impl
│   │   └── test/e2e/        # E2E tests
│   └── adk/                 # Go Agent Development Kit
│
├── python/                  # Python workspace (UV)
│   ├── packages/            # ADK packages
│   │   ├── kagent-adk/      # Main ADK
│   │   ├── kagent-core/     # Core utilities
│   │   └── kagent-skills/   # Skills framework
│   └── samples/             # Example agents
│
├── helm/                    # Kubernetes deployment
│   ├── kagent-crds/         # CRD chart (install first)
│   ├── kagent/              # Main app chart
│   └── agents/              # Pre-built agent charts
│
└── ui/                      # Next.js web interface
```

**Module Boundaries:**
- **go/api/** - Shared types used by both core and adk. Import from other modules OK.
- **go/core/** - Infrastructure code. Should NOT import from go/adk.
- **go/adk/** - Agent runtime. Can import from go/api, not from go/core.

---

## Adding CRD Fields

### Overview

Adding a field to a CRD involves several connected steps. The workflow ensures the field propagates from the CRD definition through to the Kubernetes resources that get created.

### Step-by-Step Workflow

1. **Edit the CRD type definition**

   File: `go/api/v1alpha2/agent_types.go` (or the relevant CRD type)

   ```go
   type AgentSpec struct {
       // Existing fields...

       // NewField is a description of what this field does
       // +optional
       // +kubebuilder:validation:MinLength=1
       NewField *string `json:"newField,omitempty"`
   }
   ```

   **Key points:**
   - Add kubebuilder markers for validation (`+kubebuilder:validation:...`)
   - Use `+optional` for optional fields
   - Use pointers when you need to distinguish "unset" from an empty/zero value, and follow the patterns of surrounding fields (some optional fields intentionally use value types)
   - Use `omitempty` in JSON tag for optional fields
   - Document the field with a comment explaining what it does

2. **Run code generation**

   ```bash
   make -C go generate
   ```

   This runs controller-gen to:
   - Generate DeepCopy methods (`zz_generated.deepcopy.go`)
   - Update CRD manifests in `go/api/config/crd/bases/`
   - Update RBAC manifests

3. **Copy manifests to Helm chart**

   ```bash
   cp go/api/config/crd/bases/kagent.dev_agents.yaml helm/kagent-crds/templates/
   ```

   Or use the combined target:
   ```bash
   make controller-manifests  # Does both generate + copy
   ```

4. **Update the translator (if needed)**

   File: `go/core/internal/controller/translator/agent/adk_api_translator.go`

   If your field affects the Kubernetes resources created for the agent (Deployment, Service, ConfigMap, etc.), update the translator to use it.

   Example:
   ```go
   func (a *adkApiTranslator) translateInlineAgent(...) {
       // ... existing translation code

       // Use the new field
       if agent.Spec.NewField != nil {
           deployment.Spec.Template.Spec.Containers[0].Env = append(
               deployment.Spec.Template.Spec.Containers[0].Env,
               corev1.EnvVar{
                   Name:  "NEW_FIELD",
                   Value: *agent.Spec.NewField,
               },
           )
       }
   }
   ```

   See `references/translator-guide.md` for when and how to update translators.

5. **Add E2E test**

   File: `go/core/test/e2e/invoke_api_test.go`

   Add a test case that:
   - Creates an agent with the new field set
   - Verifies it works as expected
   - Cleans up after itself

   ```go
   func TestE2ENewField(t *testing.T) {
       agent := &v1alpha2.Agent{
           ObjectMeta: metav1.ObjectMeta{
               Name:      "test-agent-" + randString(),
               Namespace: "kagent",
           },
           Spec: v1alpha2.AgentSpec{
               NewField: pointer.String("test-value"),
               // ... other required fields
           },
       }

       // Create, wait, test, cleanup
   }
   ```

6. **Run tests**

   ```bash
   # Unit tests (including translator tests)
   make -C go test

   # E2E tests
   export KAGENT_URL="http://$(kubectl get svc -n kagent kagent-controller -o jsonpath='{.status.loadBalancer.ingress[0].ip}'):8083"
   make -C go e2e
   ```

7. **Update documentation (if user-facing)**

   If the field is something users configure in their Agent YAML:
   - Add example to `examples/`
   - Update relevant README sections
   - Consider updating Helm chart values if exposed there

### Common Issues

**CRD validation errors in tests:**
- Check that kubebuilder markers are correct
- Verify JSON tags match field names (camelCase)
- Ensure required fields have values in test fixtures

**Translator tests fail (golden files mismatch):**
- Regenerate golden files: `UPDATE_GOLDEN=true make -C go test`
- Review the diff to make sure changes are intentional
- Commit the updated golden files

**Field not showing up in created resources:**
- Check that translator is using the field
- Verify field is set in test agent spec
- Look at actual created resources: `kubectl get deployment -n kagent test-agent -o yaml`

For detailed examples of different field types and validation markers, see `references/crd-workflow-detailed.md`.

---

## E2E Testing

### Environment Variables

E2E tests require specific environment variables:

**KAGENT_URL (required):**
```bash
export KAGENT_URL="http://$(kubectl get svc -n kagent kagent-controller -o jsonpath='{.status.loadBalancer.ingress[0].ip}'):8083"
echo $KAGENT_URL  # Should print something like http://172.18.0.100:8083
```

**KAGENT_LOCAL_HOST (usually auto-detected):**
Host IP accessible from inside Kind pods (for mock LLM server):
- Auto-detected: `host.docker.internal` on macOS, `172.17.0.1` on Linux
- Set explicitly if auto-detection fails: `export KAGENT_LOCAL_HOST=172.17.0.1`

**SKIP_CLEANUP (optional for debugging):**
```bash
# Preserve resources after test failure for manual inspection
SKIP_CLEANUP=1 KAGENT_URL="..." go test -v ./test/e2e/ -run TestE2EInvokeInlineAgent
```

**Common KAGENT_URL mistakes:**
- Using ClusterIP instead of LoadBalancer ingress IP
- Using localhost (doesn't work from outside the cluster)
- Forgetting the `:8083` port
- Not setting it at all (tests will try to autodiscover and likely fail)

### Prerequisites for E2E Tests

Before running E2E tests, ensure:

1. **Kind cluster with kagent deployed:**
   ```bash
   make create-kind-cluster
   make helm-install
   ```

2. **Test agent images built** (for BYO agent tests):
   ```bash
   make push-test-agent  # Builds and pushes test images
   ```
   This creates:
   - `localhost:5001/basic-openai:latest`
   - `localhost:5001/poem-flow:latest`
   - `localhost:5001/kebab:latest`

3. **Test resources deployed:**
   - `kagent-tool-server` RemoteMCPServer in `kagent` namespace
   - `kebab-agent` deployed (used by `TestE2EInvokeExternalAgent`)

### Running E2E Tests

```bash
# All tests (with shuffle for randomness)
cd go/core
KAGENT_URL="http://$(kubectl get svc -n kagent kagent-controller -o jsonpath='{.status.loadBalancer.ingress[0].ip}'):8083" \
  go test -v github.com/kagent-dev/kagent/go/core/test/e2e -failfast -shuffle=on

# Specific test
KAGENT_URL="..." go test -v github.com/kagent-dev/kagent/go/core/test/e2e -run TestE2EInvokeInlineAgent

# With cleanup disabled (for debugging)
SKIP_CLEANUP=1 KAGENT_URL="..." go test -v github.com/kagent-dev/kagent/go/core/test/e2e -run TestName
```

### How E2E Tests Work

Each test follows this pattern:
1. Starts a mock LLM server on the test host (via `mockllm`)
2. Creates K8s resources (ModelConfig, Agent, optionally MCPServer)
3. Waits for `condition=Ready` on the Agent CR
4. Polls the A2A endpoint until it returns non-5xx (via `waitForEndpoint()`)
5. Sends A2A messages (sync and/or streaming) and checks responses
6. Cleans up resources (or logs debug info on failure if `SKIP_CLEANUP` not set)

The mock LLM server runs on the test host and must be reachable from inside Kind pods using `KAGENT_LOCAL_HOST`.

### Debugging E2E Failures

**Step 1: Check cluster state**
```bash
kubectl get agents.kagent.dev -n kagent
kubectl get pods -n kagent
kubectl get svc -n kagent
```

**Step 2: Check logs**
```bash
# Controller logs
kubectl logs -n kagent deployment/kagent-controller

# Specific agent pod
kubectl logs -n kagent deployment/test-agent-xyz
```

**Step 3: Reproduce locally (without cluster noise)**

Follow the guide in `go/core/test/e2e/README.md`:

1. Extract agent config from cluster or generate one
2. Start mock LLM server with response from test
3. Run agent locally with `kagent-adk test`

This isolates the agent runtime from controller/cluster complexity.

See `references/e2e-debugging.md` for comprehensive debugging techniques.

### Golden Files

Translator tests use golden files to verify generated Kubernetes manifests.

**Location:** `go/core/internal/controller/translator/agent/testdata/outputs/`

**When to regenerate:**
- After changing translator logic
- After adding new fields that affect generated resources
- When test shows diff but it's expected

**How to regenerate:**
```bash
UPDATE_GOLDEN=true make -C go test
```

**Review before committing:**
```bash
git diff go/core/internal/controller/translator/agent/testdata/outputs/
```

Make sure changes are intentional - golden files capture the exact output, so unintended changes indicate bugs.

---

## PR Review Workflow

### Checking Out a PR

```bash
# Fetch the PR branch
gh pr checkout 1234

# Or manually
git fetch origin pull/1234/head:pr-1234
git checkout pr-1234
```

### Understanding Impact

When reviewing a PR, check if changes require updates elsewhere:

**CRD type changes** → Need:
- Codegen (should be committed)
- Helm CRD manifests (should be committed)
- Translator updates (if field affects resources)
- E2E test (if user-facing)

**Translator changes** → Need:
- Updated golden files (should be committed)
- E2E test (if behavior changes)

**API changes** → Need:
- Updated types in other modules that import
- Client SDK updates (if breaking)

**Python ADK changes** → Need:
- Sample agents updated (if breaking)
- Version bump in pyproject.toml

### Checking Conventions

**Commit messages:**
- Follow Conventional Commits: `type: description`
- Types: `feat`, `fix`, `docs`, `refactor`, `test`, `chore`, `perf`, `ci`
- Must be signed with `-s` flag
- Example: `feat: add support for custom service account in agent CRD`

**Code patterns:**
- Error handling: wrap with `fmt.Errorf("context: %w", err)`
- Kubebuilder markers: validate syntax (no Helm template syntax `{{ }}` in comments)
- Go modules: changes to go.mod should run `go mod tidy`

**Required for CRD changes:**
- [ ] Generated code updated (deepcopy, manifests)
- [ ] Helm CRD templates updated
- [ ] Golden files regenerated if translator changed
- [ ] E2E test added or updated

### Testing Changes Locally

```bash
# Build and deploy PR changes
make helm-install  # Rebuilds images with PR code

# Run relevant tests
make -C go test
export KAGENT_URL="http://$(kubectl get svc -n kagent kagent-controller -o jsonpath='{.status.loadBalancer.ingress[0].ip}'):8083"
make -C go e2e

# Check specific functionality
kubectl apply -f examples/agent-with-new-feature.yaml
kubectl get agents.kagent.dev -n kagent my-test-agent -o yaml
```

### Leaving Review Comments

Check for existing pending reviews before submitting:
```bash
gh api graphql -f query='
{
  repository(owner: "kagent-dev", name: "kagent") {
    pullRequest(number: 1234) {
      reviews(last: 10, states: PENDING) {
        nodes {
          id
          author { login }
        }
      }
    }
  }
}'
```

---

## Local Development

### Build and Deploy Cycle

**First time setup:**
```bash
make create-kind-cluster
export KAGENT_DEFAULT_MODEL_PROVIDER=openAI
export OPENAI_API_KEY=your-key
```

**Iterating on changes:**
```bash
# Full rebuild and redeploy
make helm-install

# Just rebuild controller (faster)
make build-controller
# Image is automatically pushed to local registry and used by Kind

# Redeploy without rebuild (if just changing YAML)
make helm-install-provider
```

**Quick iteration tips:**
- Controller code changes: `make build-controller` is faster than full `make build`
- Python ADK changes: `make build-app` only rebuilds Python image
- UI changes: `make build-ui` only rebuilds UI image
- Helm chart changes: `make helm-install-provider` skips image builds

### Checking Status

```bash
# High-level view
kubectl get agents.kagent.dev -n kagent
kubectl get pods -n kagent

# Detailed agent status
kubectl get agent my-agent -n kagent -o yaml | grep -A 10 status

# Watch for changes
watch kubectl get pods -n kagent

# Check if controller is processing
kubectl logs -n kagent deployment/kagent-controller -f
```

### Common Debugging Tasks

**Agent won't start:**
```bash
# Check pod status
kubectl describe pod -n kagent <agent-pod-name>

# Check init container logs (skills loading)
kubectl logs -n kagent <agent-pod-name> -c skills-init

# Check agent container logs
kubectl logs -n kagent <agent-pod-name> -c kagent
```

**Agent created but not Ready:**
```bash
# Check status conditions
kubectl get agent my-agent -n kagent -o jsonpath='{.status.conditions}' | jq

# Check controller logs for reconciliation errors
kubectl logs -n kagent deployment/kagent-controller | grep my-agent
```

**Template resolution errors:**
```bash
# Check if ConfigMap exists
kubectl get configmap -n kagent kagent-builtin-prompts

# Check Accepted condition
kubectl get agent my-agent -n kagent -o jsonpath='{.status.conditions[?(@.type=="Accepted")]}'
```

---

## CI Troubleshooting

### Common CI Failures

**1. manifests-check fails:**

Error: CRD manifests are out of date

**Fix:**
```bash
make -C go generate
cp go/api/config/crd/bases/*.yaml helm/kagent-crds/templates/
git add go/api/config/crd/bases/ helm/kagent-crds/templates/
git commit -s -m "chore: regenerate CRD manifests"
```

**2. go-lint fails with depguard errors:**

Error: package "sort" is in the denied list

**Fix:**
- Use `slices.Sort` instead of `sort.Strings`
- Check `.golangci.yml` for allowed packages
- Common denied packages: `sort` (use `slices`), `io/ioutil` (use `io` or `os`)

**3. test-e2e fails with timeout:**

**Fix:**
- Check if Kind cluster has enough resources
- Look for specific test failure in logs
- Reproduce locally with KAGENT_URL set correctly
- Check agent status: `kubectl get agents.kagent.dev -n kagent`

**4. Golden files mismatch:**

Error: translator tests fail with diff output

**Fix:**
```bash
# Regenerate and review
UPDATE_GOLDEN=true make -C go test
git diff go/core/internal/controller/translator/agent/testdata/outputs/

# If changes are correct, commit them
git add go/core/internal/controller/translator/agent/testdata/outputs/
git commit -s -m "test: regenerate golden files after <change>"
```

### Analyzing CI Logs

**Get PR check status:**
```bash
gh pr checks <pr-number>
```

**Get specific job logs:**
```bash
gh run view <run-id> --job <job-id> --log
```

**Common log patterns:**
- `Failed to create agent`: Check CRD validation, agent spec
- `context deadline exceeded`: Timeout, check if pods are starting
- `connection refused`: KAGENT_URL issue or controller not ready
- `unexpected field`: CRD schema mismatch, regenerate manifests

For comprehensive CI failure patterns, see `references/ci-failures.md`.

---

## Code Patterns

### Commit Messages

**Format:**
```
<type>: <description>

[optional body]

[optional footer]
```

**Types:**
- `feat:` - New feature
- `fix:` - Bug fix
- `docs:` - Documentation only
- `refactor:` - Code change that neither fixes a bug nor adds a feature
- `test:` - Adding or updating tests
- `chore:` - Maintenance tasks, dependencies
- `perf:` - Performance improvement
- `ci:` - CI/CD changes

**Always sign commits:**
```bash
git commit -s -m "feat: add support for X"
```

### Error Handling in Go

**Always wrap errors with context:**
```go
if err != nil {
    return fmt.Errorf("failed to create deployment for agent %s: %w", agentName, err)
}
```

**Use `%w` to preserve error chain:**
```go
if err != nil {
    return fmt.Errorf("reconciliation failed: %w", err)
}

// Later can check with errors.Is
if errors.Is(err, ErrNotFound) {
    // Handle specific error
}
```

**Return errors up, handle at boundaries:**
- Controller Reconcile: return error → requeue with backoff
- HTTP handlers: log error, return HTTP status
- CLI commands: log error, exit with code

### Kubebuilder Markers

**Validation:**
```go
// +kubebuilder:validation:Required
// +kubebuilder:validation:MinLength=1
// +kubebuilder:validation:MaxLength=255
// +kubebuilder:validation:Pattern=^[a-z0-9]([-a-z0-9]*[a-z0-9])?$
// +kubebuilder:validation:Enum=value1;value2;value3
```

**Field properties:**
```go
// +optional                    // Field is optional
// +kubebuilder:default="value" // Default value
```

**Important:** Don't use Go template syntax (`{{ }}`) in doc comments - Helm will try to parse them during CRD installation.

### Controller Patterns

**Reconcile structure:**
```go
func (r *AgentReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
    // 1. Fetch resource
    agent := &v1alpha2.Agent{}
    if err := r.Get(ctx, req.NamespacedName, agent); err != nil {
        return ctrl.Result{}, client.IgnoreNotFound(err)
    }

    // 2. Handle deletion (finalizers)
    if !agent.DeletionTimestamp.IsZero() {
        return r.handleDeletion(ctx, agent)
    }

    // 3. Reconcile (create/update child resources)
    if err := r.reconcileResources(ctx, agent); err != nil {
        return ctrl.Result{}, fmt.Errorf("failed to reconcile: %w", err)
    }

    // 4. Update status
    if err := r.Status().Update(ctx, agent); err != nil {
        return ctrl.Result{}, fmt.Errorf("failed to update status: %w", err)
    }

    return ctrl.Result{}, nil
}
```

**Requeue patterns:**
```go
// Requeue immediately (use sparingly)
return ctrl.Result{Requeue: true}, nil

// Requeue after delay
return ctrl.Result{RequeueAfter: 5 * time.Minute}, nil

// Error (exponential backoff requeue)
return ctrl.Result{}, fmt.Errorf("temporary error: %w", err)

// Success, no requeue
return ctrl.Result{}, nil
```

---

## Additional Resources

For deeper dives on specific topics, see the bundled reference files:

- `references/crd-workflow-detailed.md` - Examples of different field types, complex validation
- `references/translator-guide.md` - When and how to update translators
- `references/e2e-debugging.md` - Comprehensive E2E debugging techniques
- `references/ci-failures.md` - Common CI failure patterns and fixes
