# E2E Tests for KAgent

This directory contains end-to-end tests for KAgent features that run against a live Kubernetes cluster.

## Prerequisites

### 1. Kubernetes Cluster

You need a running Kubernetes cluster with KAgent deployed. For local testing, use Kind:

```bash
# From the kagent repository root
cd /path/to/kagent

# Create Kind cluster with local registry
make create-kind-cluster

# Set your model provider
export KAGENT_DEFAULT_MODEL_PROVIDER=openAI  # or anthropic, gemini, etc.
export OPENAI_API_KEY=your-api-key-here

# Build and deploy KAgent
make helm-install

# Verify deployment
kubectl get pods -n kagent
```

### 2. Python Dependencies

Install test dependencies:

```bash
cd /path/to/kagent

# Install Python test dependencies using uv
uv pip install pytest pytest-asyncio httpx pyyaml
```

### 3. API Access

The tests **automatically detect** the KAgent API URL from your cluster using this priority:

1. **Environment variable** (if set): `export KAGENT_API_URL=http://your-url:8083`
2. **MetalLB LoadBalancer** (automatic): Detects IP/hostname from `kagent-controller` service
3. **Fallback to localhost**: `http://localhost:8083` (requires port-forward)

#### Automatic Detection (Recommended)

If you're using Kind with MetalLB (default setup), the tests will automatically detect the LoadBalancer IP:

```bash
# No configuration needed! Tests auto-detect:
# - LoadBalancer IP from kagent-controller service
# - Service port (usually 8083)
pytest tests/e2e/test_shared_sessions.py -v -s
```

#### Manual Configuration

Override automatic detection by setting the environment variable:

```bash
export KAGENT_API_URL=http://your-custom-url:8083
pytest tests/e2e/test_shared_sessions.py -v -s
```

#### Port Forwarding (Fallback)

If LoadBalancer is not available, use port-forward:

```bash
kubectl port-forward -n kagent service/kagent-controller 8083:8083 &
pytest tests/e2e/test_shared_sessions.py -v -s
```

#### Verify Detection

The tests will print which URL they're using:

```
Detected KAGENT_API_URL from LoadBalancer: http://172.18.255.1:8083
```

or

```
Using KAGENT_API_URL from environment: http://localhost:8083
```

## Running the Tests

### Constitution Requirement (v1.1.0)

Per [KAgent Constitution 1.1.0](../../../../../.specify/memory/constitution.md), E2E tests **MUST** run against a fresh deployment:
- All project components rebuilt (`make build`)
- All Kubernetes pods restarted
- Tests verify latest code changes

**Use the provided test runner** to ensure constitution compliance:

```bash
# From the repository root
./python/packages/kagent-adk/tests/e2e/run_tests.sh

# Or from the E2E test directory
cd python/packages/kagent-adk/tests/e2e
./run_tests.sh

# Make it executable first if needed
chmod +x python/packages/kagent-adk/tests/e2e/run_tests.sh
```

The test runner automatically:
1. ✅ Verifies prerequisites (cluster, namespace, model config)
2. ✅ Detects API URL from MetalLB LoadBalancer
3. ✅ Rebuilds project (`make build`)
4. ✅ Redeploys with Helm (`make helm-install`)
5. ✅ Restarts all pods (`kubectl delete po --all`)
6. ✅ Waits for pods to be ready
7. ✅ Runs pytest with your arguments

### Development Iteration (Skip Rebuild)

For faster iteration during development, you can skip the rebuild step:

```bash
# Skip rebuild (violates constitution - dev only!)
./python/packages/kagent-adk/tests/e2e/run_tests.sh --skip-rebuild

# Skip rebuild and run specific test
./python/packages/kagent-adk/tests/e2e/run_tests.sh --skip-rebuild python/packages/kagent-adk/tests/e2e/test_shared_sessions.py::TestThreeAgentSequentialWorkflow -v
```

⚠️ **Warning**: Skipping rebuild violates Constitution 1.1.0. Use only for rapid iteration. Always run full tests before committing.

### Run All E2E Tests (Manual)

If you can't use the test runner script:

```bash
# Rebuild and redeploy first (Constitution requirement)
make build && make helm-install
kubectl delete po --all -n kagent
kubectl wait --for=condition=ready pod --all -n kagent --timeout=120s

# Then run tests
pytest python/packages/kagent-adk/tests/e2e/test_shared_sessions.py -v -s

# Or with markers
pytest python/packages/kagent-adk/tests/e2e/ -m e2e -v -s
```

### Run Specific Test Classes

```bash
# Test three-agent workflow
pytest python/packages/kagent-adk/tests/e2e/test_shared_sessions.py::TestThreeAgentSequentialWorkflow -v -s

# Test parallel workflow isolation
pytest python/packages/kagent-adk/tests/e2e/test_shared_sessions.py::TestParallelWorkflowIsolation -v -s

# Test error handling
pytest python/packages/kagent-adk/tests/e2e/test_shared_sessions.py::TestErrorHandling -v -s
```

### Run with Detailed Output

```bash
# Show print statements and detailed output
pytest python/packages/kagent-adk/tests/e2e/test_shared_sessions.py -v -s --tb=short
```

## Test Coverage

### Shared Session E2E Tests (`test_shared_sessions.py`)

Tests for the shared session ID feature in sequential workflows:

- **T044-T049**: Three-agent sequential workflow
  - Context propagation across sub-agents
  - Session creation and event persistence
  - Event ordering and author attribution
  - Context-aware decision making

- **T050**: Parallel workflow isolation
  - Verify parallel workflows use separate sessions
  - No event cross-contamination

- **T051**: Error handling
  - Error events captured in shared session
  - Session remains accessible after errors

## Test Manifests

The tests deploy the following Kubernetes resources:

- `e2e-data-collector`: Sub-agent that collects cluster data
- `e2e-analyzer`: Sub-agent that analyzes data from previous agent
- `e2e-reporter`: Sub-agent that creates summary report
- `e2e-test-sequential-workflow`: Sequential workflow coordinating the three sub-agents

These resources are automatically cleaned up after tests complete.

## Troubleshooting

### Tests Hang or Timeout

**Issue**: Tests hang waiting for agents to become ready.

**Solution**:
```bash
# Check agent status
kubectl get agents -n kagent

# Check pod logs
kubectl logs -n kagent deployment/e2e-test-sequential-workflow

# Increase timeout (edit test file)
KUBECTL_TIMEOUT = 300  # 5 minutes
```

### Connection Refused Errors

**Issue**: Tests cannot connect to KAgent API.

**Solution**:
```bash
# Verify service is running
kubectl get svc -n kagent

# Check if port-forward is active
kubectl port-forward -n kagent service/kagent-controller 8083:8083

# Export API URL
export KAGENT_API_URL=http://localhost:8083
```

### Agent Not Ready

**Issue**: Agents fail to become ready.

**Solution**:
```bash
# Check agent status
kubectl describe agent e2e-test-sequential-workflow -n kagent

# Check controller logs
kubectl logs -n kagent deployment/kagent-controller

# Verify model config exists
kubectl get modelconfig default-model-config -n kagent
```

### Session Query Returns Empty Events

**Issue**: Session query returns no events or missing events.

**Solution**:
```bash
# Check if events were created
kubectl logs -n kagent deployment/e2e-data-collector --tail=100

# Manually query session
curl -H "X-User-ID: e2e-test-user" \
  "http://localhost:8083/api/sessions/{SESSION_ID}?limit=-1"

# Check database logs
kubectl logs -n kagent deployment/kagent-controller | grep "database"
```

### Model API Errors

**Issue**: Tests fail due to LLM API errors (rate limits, authentication).

**Solution**:
```bash
# Verify API key is set
echo $OPENAI_API_KEY  # or ANTHROPIC_API_KEY, etc.

# Check model config
kubectl get modelconfig default-model-config -n kagent -o yaml

# Update model provider in tests if needed
export KAGENT_DEFAULT_MODEL_PROVIDER=anthropic
```

## Cleanup

Tests automatically clean up resources, but if needed:

```bash
# Delete test agents
kubectl delete agent -n kagent -l test=e2e

# Or manually
kubectl delete agent e2e-data-collector e2e-analyzer e2e-reporter e2e-test-sequential-workflow -n kagent

# Delete test sessions
curl -X DELETE -H "X-User-ID: e2e-test-user" \
  "http://localhost:8083/api/sessions/{SESSION_ID}"
```

## CI/CD Integration

To run these tests in CI:

```yaml
# Example GitHub Actions workflow
- name: Setup Kind Cluster
  run: make create-kind-cluster

- name: Deploy KAgent
  run: make helm-install
  env:
    KAGENT_DEFAULT_MODEL_PROVIDER: openAI
    OPENAI_API_KEY: ${{ secrets.OPENAI_API_KEY }}

- name: Run E2E Tests
  run: pytest tests/e2e/ -m e2e -v --tb=short
```

## Contributing

When adding new E2E tests:

1. Follow the existing test structure (fixtures, helper classes)
2. Use meaningful test names that describe what is being tested
3. Clean up resources in fixtures or teardown
4. Add documentation in this README
5. Use pytest markers (`@pytest.mark.e2e`, `@pytest.mark.asyncio`)

## References

- [Quickstart Guide](../../specs/002-shared-session-id/quickstart.md)
- [Feature Spec](../../specs/002-shared-session-id/spec.md)
- [Tasks](../../specs/002-shared-session-id/tasks.md)

