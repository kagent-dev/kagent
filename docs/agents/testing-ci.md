# Kagent Testing & CI Guide

Test suites, CI workflows, test data, and local testing commands.

**See also:** [go-guide.md](go-guide.md) (Go testing patterns), [python-guide.md](python-guide.md) (Python testing), [ui-guide.md](ui-guide.md) (UI testing)

---

## Test suites

### Go tests

| Suite | Command | Scope |
|-------|---------|-------|
| Unit tests | `make -C go test` | All Go packages (skips E2E) |
| E2E tests | `make -C go e2e` | Full stack with Kind cluster |
| Lint | `make -C go lint` | golangci-lint v2.11.3+ |
| Vulnerability | `make -C go govulncheck` | Known CVE check |

**Go unit tests** use table-driven patterns with race detection (`-race` flag in CI).

**Go E2E tests** require a running Kind cluster:

```bash
make create-kind-cluster    # Create cluster
make helm-install           # Deploy kagent
make -C go e2e              # Run E2E (failfast)
```

E2E tests are in `go/core/test/e2e/`.

### Python tests

| Suite | Command | Scope |
|-------|---------|-------|
| All packages | `make -C python test` | pytest across all packages |
| Format check | `make -C python lint` | Ruff formatter diff |
| Security audit | `make -C python audit` | pip-audit CVE check |

**Python tests** require test certificates: `make -C python generate-test-certs`

CI tests against multiple Python versions: 3.10, 3.11, 3.12, 3.13.

### Helm tests

Helm chart unit tests run in CI:

```bash
# Tested in CI via helm-unit-tests job
```

### UI tests

| Suite | Command | Scope |
|-------|---------|-------|
| Lint | `cd ui && npx next lint` | ESLint |
| Unit tests | `cd ui && npx jest` | Jest component tests |
| E2E tests | Cypress | Browser automation |

## CI workflow matrix

Main CI pipeline: `.github/workflows/ci.yaml`

| Job | Trigger | What it does |
|-----|---------|--------------|
| `setup` | All | Cache key generation |
| `test-e2e` | Push/PR | Kind cluster E2E tests (SQLite + Postgres matrix) |
| `go-unit-tests` | Push/PR | Go unit tests with race detection |
| `helm-unit-tests` | Push/PR | Helm chart testing |
| `ui-tests` | Push/PR | Node.js lint + Jest |
| `build` | Push/PR | Multi-platform Docker builds (amd64/arm64) |
| `go-lint` | Push/PR | golangci-lint v2.11.3 |
| `python-test` | Push/PR | Pytest across Python 3.10-3.13 |
| `python-lint` | Push/PR | Ruff linter + formatter |
| `manifests-check` | Push/PR | Verify CRD manifests are up-to-date |

### E2E test database matrix

E2E tests run against both database backends:

| Database | CI Job |
|----------|--------|
| SQLite | `test-e2e` (sqlite matrix) |
| PostgreSQL (pgvector) | `test-e2e` (postgres matrix) |

### Docker image builds

CI builds images for:
- `controller` (Go controller + HTTP server)
- `ui` (Next.js frontend)
- `app` (Python ADK runtime)
- `cli` (Go CLI binary)
- `golang-adk` (Go ADK runtime)
- `skills-init` (Skills initialization container)

Platforms: linux/amd64, linux/arm64

## Other CI workflows

| Workflow | Purpose |
|----------|---------|
| `tag.yaml` | Release tagging & versioning |
| `image-scan.yaml` | Container image vulnerability scanning |
| `run-agent-framework-test.yaml` | Framework integration tests |
| `stalebot.yaml` | Issue/PR stale detection |

## Testing requirements for PRs

### Must have

- Unit tests for new functions/methods
- E2E tests for new CRD fields or API endpoints
- Mock external services (LLMs, K8s API) in unit tests
- All existing tests passing

### Coverage rules

| Change size | Expected tests |
|-------------|----------------|
| Bug fix (< 50 lines) | Unit test reproducing bug + fix |
| Small feature (50-200) | Unit tests + integration if API-facing |
| Medium feature (200-500) | Unit + integration + E2E |
| Large feature (500+) | Unit + integration + E2E + negative tests |

### CRD change test coverage

When adding CRD fields:

1. Unit test for translator handling the new field
2. Unit test for database model handling
3. E2E test verifying end-to-end flow (CRD → agent config → runtime behavior)

### Type alignment test coverage

When changing types in `go/api/adk/types.go`:

1. Verify Python mirror in `kagent-adk/src/kagent/adk/types.py` is updated
2. Add test that serializes Go type to JSON and deserializes in Python
3. Verify `config.json` schema is backward compatible

## Local CI reproduction

Reproduce CI failures locally:

```bash
# Go tests
make -C go test                    # Unit tests
make -C go lint                    # Linting
make -C go manifests generate      # CRD generation check

# Python tests
make -C python generate-test-certs # Required for TLS tests
make -C python test                # All pytest
make -C python lint                # Ruff formatting

# UI tests
cd ui && npm ci && npx next lint && npx jest

# E2E (requires Kind cluster)
make create-kind-cluster
make helm-install
make -C go e2e

# Manifests check
make -C go manifests generate
git diff --exit-code go/api/config/
```

## Common CI failure patterns

| Failure | Fix |
|---------|-----|
| `golangci-lint` error | Run `make -C go lint-fix` locally |
| `manifests-check` failed | Run `make -C go manifests generate` and commit |
| Python format error | Run `make -C python format` and commit |
| E2E timeout | Check pod logs: `kubectl logs -n kagent deploy/kagent-controller` |
| TLS test failure | Run `make -C python generate-test-certs` |
| Race condition in Go tests | Look for shared state in test setup/teardown |
