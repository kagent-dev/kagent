# CI Pipeline Analysis

## Current State

- CI file: `.github/workflows/ci.yaml`
- Job `test-e2e` runs on ubuntu-latest with matrix (sqlite, postgresql)
- Uses Kind cluster + Helm install
- Runs `go test -v github.com/kagent-dev/kagent/go/core/test/e2e -failfast -shuffle=on`
- **No TEMPORAL_ENABLED flag** → all temporal tests are skipped

## Existing Makefile Targets

```makefile
# go/Makefile
e2e-temporal:
    cd core && TEMPORAL_ENABLED=1 go test -v -run 'TestE2ETemporal.*' ... -failfast

# Root Makefile
helm-install-temporal: KAGENT_HELM_EXTRA_ARGS+=--set temporal.enabled=true --set nats.enabled=true ...
helm-install-temporal: helm-install
```

Both targets exist but are not wired into CI.

## Helm Infrastructure

Templates exist and are ready:
- `temporal-server-deployment.yaml` — temporalio/auto-setup:1.26.2 (port 7233)
- `temporal-ui-deployment.yaml` — temporalio/ui:2.34.0 (port 8080)
- `nats-deployment.yaml` — nats:2-alpine (port 4222)
- Services for all three
- Controller ConfigMap injects TEMPORAL_HOST_ADDR and NATS_ADDR when temporal.enabled

SQLite mode for dev/CI: uses `temporal server start-dev --headless` with emptyDir volume.

## Docker Images

- `golang-adk` — already built in CI build matrix
- `temporal-mcp` — plugin image, built via `make build-temporal-mcp`
- Both pushed to local registry (localhost:5001) for Kind

## What's Needed for CI Integration

### Option A: Separate Job
Add `test-e2e-temporal` job to ci.yaml:
1. Reuse Kind setup from existing job
2. `make helm-install-temporal` instead of `make helm-install`
3. Set `TEMPORAL_ENABLED=1`
4. Run `make -C go e2e-temporal`

### Option B: Extend Matrix
Add `temporal: [false, true]` to existing matrix, conditionally:
- Use `helm-install-temporal` when temporal=true
- Set TEMPORAL_ENABLED=1 when temporal=true

### Timing Impact
- Temporal server readiness: ~30-40s
- NATS readiness: ~10s
- Total additional time: ~3-4 min per job

### Files to Modify
- `.github/workflows/ci.yaml` — add job or matrix dimension
- No other file changes needed (Makefile targets + Helm templates already exist)
