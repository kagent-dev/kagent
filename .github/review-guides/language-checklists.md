# Language-Specific Review Checklists

Load when reviewing code changes. Pick the relevant language section.

---

## Go (controllers, API, ADK)

### Error handling and control flow
- Every `err :=` must be checked or returned. Swallowed errors cause silent failures.
- Wrap errors: `fmt.Errorf("context: %w", err)`
- `return` inside loops: verify it should be `continue`, not premature exit
- Loops assigning to a variable: verify intermediate values aren't discarded
- No variable shadowing of function arguments
- No unnecessary `else` after `return`

### Concurrency
- No nested goroutines (`go func() { go func() { ... } }()`)
- Reuse K8s clients from struct fields, not `kubernetes.NewForConfig()` per call
- Fire-and-forget goroutines: require `context.WithTimeout` + error logging
- Database operations: atomic upserts, no application-level mutexes

### Code quality
- `golangci-lint run` passes
- Context propagation (`context.Context` as first parameter)
- Resource cleanup (`defer` close for readers/connections)
- Table-driven tests for new functions
- Descriptive variable names (`fingerPrint` not `fp`, `cacheKey` not `ck`)
- camelCase for locals, PascalCase for exports
- No deprecated fields in new code

### CRD-specific
- New fields have JSON tags matching the Go field name (camelCase)
- `+optional` marker for optional fields
- DeepCopy generated (`make -C go generate`)
- CRD manifests regenerated (`make -C go manifests`)

## Python (ADK, skills, integrations)

- Type hints on all function signatures
- No bare `Any` types without justification
- Ruff formatting compliance
- `async/await` used consistently (no mixing sync and async)
- Cross-language types: add `# Must stay aligned with Go type in ...` comments
- No bare `Exception` catches — use specific exception types
- No mutable default arguments
- Tests use `pytest` with `@pytest.mark.asyncio` for async tests

## TypeScript (UI)

- Strict mode compliance (no `any` type)
- No inline styles — use TailwindCSS classes
- No direct DOM manipulation — use React patterns
- Radix UI primitives for accessibility
- React Hook Form + Zod for form validation
- Jest tests for logic-heavy components
- ESLint + Next.js lint passing

## YAML (Helm charts, CI)

- Helm templates use proper `.Values` references
- No hardcoded image tags — use chart values
- CI workflows: no secrets in logs, pinned action versions
- CRD templates match generated manifests in `go/api/config/crd/`
