# CI Failure Patterns and Fixes

Common GitHub Actions CI failures and how to fix them.

## Quick Reference

| Failure | Likely Cause | Quick Fix |
|---------|--------------|-----------|
| manifests-check | CRD manifests out of date | `make -C go generate && cp go/api/config/crd/bases/*.yaml helm/kagent-crds/templates/` |
| go-lint depguard | Forbidden package used | Replace with allowed alternative (e.g., `slices.Sort` not `sort.Strings`) |
| test-e2e timeout | Agent not starting or KAGENT_URL wrong | Check pod status, verify KAGENT_URL setup in CI |
| golden files mismatch | Translator output changed | `UPDATE_GOLDEN=true make -C go test` and commit |
| go-unit-tests fail | Test code broken | Fix test, ensure mocks are correct |
| python-tests fail | Python ADK broken | Fix code, check UV dependencies |
| build failure | Compilation error | Fix syntax/type errors |
| helm-lint | Invalid Helm chart | Fix chart syntax, run `helm lint helm/kagent` |

---

## manifests-check Failures

### Error Pattern

```
Error: CRD manifests are out of date
Found differences in:
  helm/kagent-crds/templates/kagent.dev_agents.yaml
```

### Root Cause

CRD type definitions changed but generated manifests weren't updated.

### Fix

```bash
# Regenerate manifests and copy to Helm chart in one step
make controller-manifests

# Commit
git add go/api/config/crd/bases/ helm/kagent-crds/templates/
git commit -s -m "chore: regenerate CRD manifests"
git push
```

### Prevention

Always run `make controller-manifests` after modifying types in `go/api/v1alpha2/`.

Add to PR checklist:
- [ ] Ran `make controller-manifests` after CRD changes
- [ ] Committed generated files

---

## go-lint Failures

### Error Pattern 1: depguard

```
Error: package "sort" is in the denied list (depguard)
  go/internal/controller/agent_controller.go:123:5
```

**Cause:** Using forbidden package

**Forbidden packages:**
- `sort` → Use `slices` instead
- `io/ioutil` → Use `io` or `os` instead
- Others in `.golangci.yml`

**Fix:**
```go
// Bad
import "sort"
sort.Strings(items)

// Good
import "slices"
slices.Sort(items)
```

### Error Pattern 2: unused variable

```
Error: unused variable 'result' (unused)
  go/internal/controller/agent_controller.go:45:2
```

**Fix:**
```go
// If truly unused, remove it
// result := doSomething()  // Remove this line

// If needed later, use blank identifier temporarily
_ = doSomething()

// Or fix logic to use it
result := doSomething()
return result
```

### Error Pattern 3: error not checked

```
Error: Error return value is not checked (errcheck)
  go/internal/controller/agent_controller.go:67:2
```

**Fix:**
```go
// Bad
client.Update(ctx, agent)

// Good
if err := client.Update(ctx, agent); err != nil {
    return fmt.Errorf("failed to update agent: %w", err)
}
```

### Running Lint Locally

```bash
# All modules
make -C go lint

# Auto-fix some issues
make -C go lint-fix

# Check specific file
cd go/core
golangci-lint run internal/controller/agent_controller.go
```

---

## test-e2e Failures

### Error Pattern 1: Timeout

```
Error: context deadline exceeded
  github.com/kagent-dev/kagent/go/core/test/e2e.TestE2EInvokeInlineAgent
```

**Common causes:**
1. Agent pod not starting
2. Controller not processing
3. MCP server not responding

**Debug in CI logs:**
```bash
# Look for kubectl output in CI logs
grep "kubectl get pods" ci-log.txt
grep "kubectl describe" ci-log.txt
```

**Fix:**
- Check "fail print info" step in CI for diagnostic output
- Look for pod events showing why pod didn't start
- Increase timeout if legitimately slow in CI

### Error Pattern 2: Connection Refused

```
Error: dial tcp 172.18.0.100:8083: connect: connection refused
```

**Cause:** KAGENT_URL points to wrong IP or controller not ready

**Fix in CI:**
Verify `.github/workflows/ci.yaml` has correct KAGENT_URL setup:
```yaml
export KAGENT_URL="http://$(kubectl get svc -n kagent kagent-controller -o jsonpath='{.status.loadBalancer.ingress[0].ip}'):8083"
```

### Error Pattern 3: Unexpected Field

```
Error: Agent.kagent.dev "test-agent" is invalid: spec.declarative.newField: field not found
```

**Cause:** CRD in cluster doesn't have new field

**Fix:**
1. Ensure `make controller-manifests` was run and files committed
2. Ensure CRD chart was upgraded in `Install Kagent` step

---

## Golden File Failures

### Error Pattern

```
FAIL: TestTranslateAgent/agent_with_new_field (0.01s)
    translate_test.go:123: golden file mismatch
        Expected: testdata/outputs/agent_with_new_field.json
        Got: /tmp/actual-output.json

        Diff:
        --- expected
        +++ actual
        @@ -45,6 +45,7 @@
           "env": [
             {"name": "MODEL_API_KEY", "value": "test-key"},
        +    {"name": "NEW_ENV_VAR", "value": "new-value"}
           ]
```

**Cause:** Translator output changed (could be intentional or bug)

**If intentional (you changed translator):**
```bash
UPDATE_GOLDEN=true make -C go test
git add go/core/internal/controller/translator/agent/testdata/outputs/
git commit -s -m "test: regenerate golden files after translator change"
```

**If unintentional (bug):**
Fix translator logic, test again

**Prevention:**
- Always regenerate golden files after translator changes
- Review diff carefully before committing
- Include golden file updates in same commit as translator change

---

## Build Failures

### Error Pattern 1: Undefined Variable

```
Error: undefined: newFunction
  go/internal/controller/agent_controller.go:123:5
```

**Cause:** Referenced something that doesn't exist

**Fix:**
- Add missing import
- Fix typo
- Implement missing function

### Error Pattern 2: Type Mismatch

```
Error: cannot use agent.Spec.NewField (type *string) as type string in assignment
  go/internal/controller/translator/agent/adk_api_translator.go:234:15
```

**Fix:**
```go
// Bad
value := agent.Spec.NewField  // *string

// Good
value := *agent.Spec.NewField  // string (but check for nil first!)

// Better
if agent.Spec.NewField != nil {
    value := *agent.Spec.NewField
}
```

### Error Pattern 3: Import Cycle

```
Error: import cycle not allowed
  package go/core/internal/controller imports go/adk/pkg/agent
  package go/adk/pkg/agent imports go/core/internal/types
```

**Fix:**
- Move shared types to `go/api/`
- Restructure imports to break cycle
- Use interfaces to invert dependency

---

## Python Test Failures

### Error Pattern 1: Import Error

```
ImportError: cannot import name 'NewClass' from 'kagent.adk'
```

**Cause:** Missing `__init__.py` export or module not installed

**Fix:**
```bash
# Reinstall in development mode
cd python
uv sync --all-packages

# Check __init__.py exports
grep "NewClass" packages/kagent-adk/src/kagent/adk/__init__.py
```

### Error Pattern 2: Ruff Formatting

```
Error: Ruff format check failed
  packages/kagent-adk/src/kagent/adk/agent.py:45:1: line too long (120 > 100)
```

**Fix:**
```bash
# Auto-format
cd python
uv run ruff format .

# Check again
uv run ruff check .
```

### Error Pattern 3: Type Errors

```
Error: Incompatible types in assignment (expression has type "str", variable has type "int")
```

**Fix:** Fix type annotations or actual code

---

## Docker Build Failures

### Error Pattern 1: Base Image Not Found

```
Error: failed to solve: base-image:tag: not found
```

**Cause:** Base image tag changed or doesn't exist

**Fix:** Update `Dockerfile` with correct base image tag

### Error Pattern 2: COPY Failed

```
Error: COPY failed: file not found in build context
```

**Cause:** File being copied doesn't exist

**Fix:**
- Ensure file exists at that path
- Check `.dockerignore` isn't excluding it
- Verify build context includes the file

---

## Helm Lint Failures

### Error Pattern 1: Invalid YAML

```
Error: error unmarshaling JSON: while decoding JSON: json: cannot unmarshal string into Go value of type int
```

**Cause:** YAML type mismatch

**Fix in `helm/kagent/values.yaml` or templates:**
```yaml
# Bad
replicas: "1"  # String

# Good
replicas: 1  # Integer
```

### Error Pattern 2: Missing Required Value

```
Error: render error in "kagent/templates/deployment.yaml": template: kagent/templates/deployment.yaml:45:22: executing "kagent/templates/deployment.yaml" at <.Values.required.value>: nil pointer evaluating interface {}.value
```

**Cause:** Required value not set

**Fix:**
```yaml
# In values.yaml
required:
  value: "default-value"
```

Or use `required` function:
```yaml
{{ required "value is required" .Values.required.value }}
```

---

## Flaky Tests

**Pattern:** Test passes sometimes, fails others

**Common causes:**

1. **Race conditions**
   ```go
   // Bad: no synchronization
   go func() { counter++ }()
   assert.Equal(t, 1, counter)  // May be 0 or 1

   // Good: use channels or sync
   done := make(chan struct{})
   go func() {
       counter++
       close(done)
   }()
   <-done
   assert.Equal(t, 1, counter)
   ```

2. **Timing assumptions**
   ```go
   // Bad: assumes operation completes in 1 second
   doAsyncOperation()
   time.Sleep(1 * time.Second)
   assert.True(t, operationComplete)

   // Good: use Eventually
   doAsyncOperation()
   require.Eventually(t, func() bool {
       return operationComplete
   }, 10*time.Second, 100*time.Millisecond)
   ```

3. **Resource contention**
   - Multiple tests creating resources with same name
   - Solution: Use random names (`randString()`)

4. **Order dependencies**
   - Test passes when run alone, fails in suite
   - Solution: Ensure tests are independent, clean up resources

---

## Analyzing CI Logs

### Finding the Failure

```bash
# List checks for a PR
gh pr checks <pr-number>

# View specific run
gh run view <run-id>

# Get logs
gh run view <run-id> --job <job-id> --log > ci-log.txt
```

### Key Sections to Check

**1. Build phase:**
```bash
grep -A 20 "Building" ci-log.txt
```

**2. Test phase:**
```bash
grep -A 20 "FAIL:" ci-log.txt
```

**3. Post-failure diagnostics:**
```bash
grep -A 50 "fail print info" ci-log.txt
```

This shows kubectl output when tests fail.

**4. Error summary:**
```bash
grep "Error:" ci-log.txt
grep "FAIL" ci-log.txt
```

---

## Prevention Strategies

### Pre-commit Checks

Run locally before pushing:

```bash
# 1. Generate if needed
make -C go generate

# 2. Lint
make -C go lint
make -C python lint

# 3. Unit tests
make -C go test

# 4. E2E tests (if cluster available)
export KAGENT_URL="..."
make -C go e2e
```

### Git Hooks

Set up pre-commit hook:

```bash
# Initialize repo hooks
make init-git-hooks

# This sets up hooks from .githooks/
```

### PR Checklist

Before submitting PR:

- [ ] Ran `make -C go generate` after CRD changes
- [ ] Ran `make lint` and fixed issues
- [ ] Ran `make -C go test` and all pass
- [ ] Regenerated golden files if translator changed
- [ ] E2E tests pass locally
- [ ] Commits are signed (`-s` flag)
- [ ] Conventional commit format used

---

## Common CI Environment Differences

### Resource Limits

CI has limited resources:
- Less memory → Some tests may OOM
- Less CPU → Slower builds/tests
- Solution: Adjust resource requests/limits

### Network Configuration

CI uses different network setup:
- Kind cluster networking may differ
- Solution: Verify KAGENT_URL setup matches CI

### Timing

CI is often slower:
- Builds take longer
- Tests take longer
- Solution: Increase timeouts in CI-specific cases

### Permissions

CI has limited permissions:
- Can't access certain resources
- Can't perform certain operations
- Solution: Use mocks or skip tests that need special permissions

---

## Getting Help

If you can't figure out the CI failure:

1. **Check recent similar PRs** - Maybe same failure occurred before
   ```bash
   gh pr list --state all --label "ci-failure"
   ```

2. **Download and analyze logs**
   ```bash
   gh run download <run-id>
   grep -r "Error" <run-dir>/
   ```

3. **Ask in PR comments** - Tag maintainers with CI logs

4. **Check GitHub Actions status** - Sometimes it's GitHub's issue
   https://www.githubstatus.com/
