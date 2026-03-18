# Test Quality Review Guide

Load when reviewing PRs that add/modify tests, or should include tests.

---

## Coverage requirements

- Every new public function: happy path + edge cases + error paths
- New controller reconciliation paths: unit tests with fake K8s client
- New HTTP API endpoints: integration tests
- New CRD fields: translator unit tests + E2E test
- New algorithmic code (BFS, tree walks, parsing): dedicated unit tests — E2E alone is insufficient

## Proportional coverage

| Change size | Expected |
|-------------|----------|
| Bug fix (< 50 lines) | Unit test reproducing bug + fix |
| Small feature (50-200) | Unit tests + integration if API-facing |
| Medium feature (200-500) | Unit + integration + E2E |
| Large feature (500+) | Unit + integration + E2E + negative tests |

## Type alignment coverage

When changing types across Go and Python:

- Verify both sides are updated
- Add serialization round-trip tests where possible
- Verify backward compatibility (old config.json still parseable)

## Test organization

- Go: table-driven tests in `_test.go` files alongside source
- Python: `pytest` tests in package test directories
- UI: Jest tests in `__tests__/` directories or `.test.tsx` files
- E2E: `go/core/test/e2e/` for full-stack tests
- Utilities go in shared test helpers, not individual test files

## Assertions and matchers

- Assertion failure messages must match what the assertion actually checks
- Use specific assertions over generic (check exact values, not just nil)
- Go: use `t.Errorf` with descriptive messages including got/want
- Python: use pytest assertions with `-v` for detailed output
- TypeScript: use Jest matchers (`toEqual`, `toHaveBeenCalledWith`)

## Test naming

- Go: `TestFunctionName_Scenario` (e.g., `TestReconcile_AgentNotFound`)
- Python: `test_function_name_scenario` (e.g., `test_executor_handles_timeout`)
- Descriptive names that explain the scenario being tested

## Fixture integrity

- Never modify shared fixtures for new features — create new ones
- Tests must not depend on execution order
- Clean up resources in teardown, not via namespace deletion
- Sleep durations: 5s max (not 20s+); prefer polling with timeout
- No hardcoded paths or env-specific values

## Resource management

- Go: `defer` cleanup for test resources (clients, connections, temp files)
- Python: `pytest` fixtures with `yield` for setup/teardown
- E2E: verify pod cleanup after test completion
- Tests parallel-safe (no name collisions, no shared mutable state)

## CI quality

- Top-level comment explaining test purpose
- CI matrix job names include all parameters
- New dependencies: justified, maintained, license-compatible
- Test data in `testdata/` directories, not inline
