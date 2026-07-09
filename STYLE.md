# Kagent Style Guide

This guide documents the coding conventions for the kagent project. It applies to
both human contributors and AI agents. It exists so that reviews can focus on
design and correctness rather than repeating the same style feedback.

The key words **MUST**, **MUST NOT**, **SHOULD**, **SHOULD NOT**, and **MAY** are
to be interpreted as described in [RFC 2119](https://www.rfc-editor.org/rfc/rfc2119):
**MUST** rules are enforced in review; **SHOULD** rules require justification to
deviate from.

Examples are marked ✓ (do this) and ✗ (don't do this).

---

## Table of Contents

1. [General Principles (all languages)](#general-principles)
2. [Go](#go)
3. [Python](#python) *(TODO)*
4. [TypeScript](#typescript) *(TODO)*
5. [Helm / YAML](#helm--yaml) *(TODO)*

---

## General Principles

These apply to every language in the repository.

### Keep PRs focused

- A PR **MUST** change one thing. Don't mix feature work with unrelated cleanup,
  devcontainer changes, sample agents, or changes carried over from other branches.
- Large mechanical changes (renames, generated code) **SHOULD** be split from
  behavioral changes.

### Never swallow errors

- Errors **MUST** be propagated or explicitly handled — never silently discarded.
  If the entire purpose of a code path is to record or persist something, a
  failure in that path is an error, not a log line.
- If an error genuinely cannot be propagated, it **MUST** at minimum be logged
  with enough context to debug it.

### DRY — extract shared helpers

- Logic repeated in two or more places **SHOULD** be extracted into a shared
  helper. Three or more repetitions **MUST** be extracted.
- Before writing a helper, check whether one already exists in the codebase.

### Avoid premature abstraction

- Do not introduce options patterns, wrapper types, interfaces, or abstraction
  layers for trivial cases. The complexity cost must be justified by real,
  present need — not hypothetical future need.

  > "Do we really need to introduce an `options` pattern for 2 strings?"

- Symmetrically: don't keep an abstraction that only has one implementation and
  no near-term prospect of a second, unless it's needed for testing.

### Reuse before reinventing

- Before creating a new type or utility, check: the standard library, upstream
  libraries already in use, the Kubernetes API, and the rest of this codebase.
  Mirroring or reimplementing an existing type **MUST NOT** be done without a
  documented reason.

### Remove dead code immediately

- Unused imports, arguments, variables, functions, files, and binary entry
  points **MUST** be deleted, not left in "for later". Dead code creates
  confusion about what is actually used. Git history preserves it if needed.

### Minimize exported surface

- Only export symbols that callers outside the package genuinely need. Ask:
  "Does anyone outside this package need this?" If the answer is "only tests",
  test from within the package instead.

### Name things by user semantics, not implementation

- API fields, endpoint paths, and type names **MUST** reflect what users
  understand, not internal implementation details.

  ✗ Exposing user-facing agents as `teams` because the runtime calls them that.
  ✗ Naming a reference field `image` when it's semantically a `reference`/`uri`.

### Constants for magic strings

- Annotation keys, label keys, condition reasons, and other repeated string
  literals **MUST** be defined as named constants — in the API types package
  when they are part of the API surface.

---

## Go

Go code lives in the `go/` workspace (`go/api`, `go/core`, `go/adk`). Run
`make -C go lint` before submitting; the golangci-lint config in
`go/.golangci.yaml` enforces a number of the rules below mechanically.

### Package layout

- Use `internal/` aggressively. Code goes in `internal/` by default; only
  intentionally reusable packages go in `pkg/`.
- Shared types (CRDs, DB models, HTTP API shapes, client) live in `go/api`.
- When a package accumulates unrelated types or behaviors, split it into
  descriptive sub-packages (e.g. `translator/agent`, `translator/mcp`).
  Don't let a package become a catch-all.
- K8s imports **MUST** use the standard aliases (enforced by `importas`):
  `corev1`, `metav1`, `appsv1`, `apierrors`, `kerrors`, etc.

### Typed data over generic maps

- When the shape of data is known at compile time, define a named struct.
  `map[string]interface{}` **MUST NOT** be used for data whose schema you
  control; it is reserved for genuinely schema-less JSON.

  ```go
  // ✗
  cfg := map[string]interface{}{"model": "gpt-4o", "temperature": 0.2}

  // ✓
  type ModelParams struct {
      Model       string  `json:"model"`
      Temperature float64 `json:"temperature"`
  }
  ```

### Error handling

- Errors **MUST** be wrapped with context using `%w`. Message pattern:
  `"failed to <verb> <noun> <identifier>: %w"`.

  ```go
  // ✓
  return fmt.Errorf("failed to get agent %s: %w", req.NamespacedName, err)
  ```

- Sentinel errors are package-level `var ErrXxx = errors.New(...)`, matched
  with `errors.Is()`.
- When callers need to distinguish an error *category*, define a custom error
  type with an `Unwrap() error` method (see `translator/agent.ValidationError`,
  `httpserver/errors.APIError`).
- Use `hashicorp/go-multierror` to accumulate multiple sub-errors when a loop
  must continue past individual failures (e.g. reconciling multiple secrets).
- Deprecated APIs **MUST NOT** be used (`ioutil` → `os`, etc.). Prefer standard
  library utilities (`slices.Equal`, `bufio.Scanner` for streaming reads) over
  hand-rolled implementations. Note the `sort` package is banned by `depguard`;
  use `slices`.

### Interfaces

- Keep interfaces small and define them where they are **consumed**, not where
  they are implemented.
- Every concrete implementation **MUST** carry a compile-time assertion:

  ```go
  var _ sandboxbackend.Backend = (*AgentsBackend)(nil)
  ```

- Accept interfaces, return structs (return the interface only when the
  concrete type is intentionally hidden, as with `NewKagentReconciler`).

### Methods vs functions

- A method receiver is justified only when the function (a) uses struct state,
  or (b) implements an interface. Otherwise it **MUST** be a plain (possibly
  exported) package function.

  > "Methods are important when you want to either hide the implementation of
  > an interface, or you need to use some long-lived item in a struct. In this
  > case it's neither, so really it should just be a public utility function."

### Constructors and dependency injection

- Dependencies **MUST** be injected at construction time via `NewXxx(deps...)`
  parameters — never via setters or lazy initialization that forces `nil`
  checks later.

  ```go
  // ✗ set later, check nil everywhere
  r := NewRegistrar()
  r.SetClientRegistry(reg)

  // ✓ inject up front
  r := NewRegistrar(reg)
  ```

- Use a `XxxConfig` value struct once a constructor exceeds ~5 parameters
  (see `adk/pkg/app.AppConfig`). Do not add functional options for a handful
  of fields.
- Keep exactly **one** constructor per type with all parameters; don't
  accumulate `NewXxxWithYyy` convenience shims.
- Related HTTP handlers share dependencies via an embedded `*Base` struct
  (see `httpserver/handlers`).
- Configuration for Go binaries **SHOULD** be CLI flags grouped into config
  structs — not an ever-growing set of individual env vars.

### Context propagation

- `context.Context` is always the **first** parameter of any function that does
  I/O (K8s, DB, network) and **MUST** be threaded through the entire call chain.
  HTTP calls use `http.NewRequestWithContext`. Never drop a `cancel` function.
- Context keys use unexported struct types, never strings.

### Logging

- Structured logging only (`logr` via controller-runtime); key-value pairs,
  never `fmt.Sprintf` inside a log call, never `fmt.Printf` for logging.
- Controllers/HTTP: `ctrllog.FromContext(ctx).WithName("...").WithValues(...)`.
  ADK: `logr.FromContextOrDiscard(ctx)`.
- Verbose or per-item output **MUST** be debug level (`log.V(1)` or higher),
  not `Info`. If the information is already returned to the caller (e.g. an
  HTTP response), don't also log it at info level.

### Naming

- Receiver names: single letter, the **first letter of the concrete type name**,
  used consistently across all methods of that type (`k` for `kagentReconciler`,
  `a` for `AgentController`, `m` for `ModelConfigHandler`).
- Acronyms are uppercase: `URL`, `ID`, `HTTP`, `MCP`, `ADK`, `TLS`, `API`.
- Getters are `GetXxx()`.
- CRD enum constants use the `TypeName_Value` underscore pattern:

  ```go
  // ✓ (kagent convention for CRD enums)
  const (
      AgentType_Declarative AgentType = "Declarative"
      AgentType_BYO         AgentType = "BYO"
  )
  ```

- Boolean fields **SHOULD** be named so the zero value is the default
  (`DisableSystemCAs`, not `UseSystemCAs` defaulting to true).

### Pointer vs value semantics

- Maps and slices are already reference types — `*map[K]V` is almost never correct. There are a vanishingly small number of cases where a pointer to a slice is justified.
- Don't deep-copy unless you actually mutate the original; don't dereference a
  pointer just to take its address again.
- Optional CRD fields **MUST** be pointers (see [CRD API design](#crd-api-design)).

### CRD API design

All new API surface goes in `v1alpha2`. `v1alpha1` **MUST NOT** receive new
features.

- Every field carries explicit markers: `// +optional` or `// +required`,
  plus `// +kubebuilder:default=...`, `// +kubebuilder:validation:Enum=...`
  where applicable. The custom `kubeapilinter` enforces much of this.
- Optional fields **MUST** be pointer types with `omitempty`. Removing
  `omitempty` makes a field required — never do this accidentally.
- `oneOf` semantics (exactly/at most one of several fields set) **MUST** be
  enforced with CEL `XValidation` rules at the CRD level, not with webhooks
  and not left to controller runtime checks:

  ```go
  // ✓
  // +kubebuilder:validation:XValidation:message="only one of stdio or http may be set",rule="[has(self.stdio), has(self.http)].filter(x, x).size() <= 1"
  ```

- Use standard K8s types before inventing new ones: `[]metav1.Condition` for
  status (with `meta.SetStatusCondition`), `corev1.LocalObjectReference` /
  `TypedLocalObjectReference` for references. Start with single-namespace
  (local) references; widen later only if needed.
- References **MUST** be structured fields, not opaque `"namespace/name"`
  strings that callers parse.
- Keep the API small: don't add convenience/sugar fields to status that can be
  expressed as conditions.
- Shared sub-specs use embedded structs with `json:",inline"` (e.g.
  `BaseModelConfig` embedded in provider-specific configs).
- Go interfaces in API packages carry `// +kubebuilder:object:generate=false`.
- When unsure how to model something (polymorphism, oneOf, selectors,
  conditions), the [Gateway API](https://github.com/kubernetes-sigs/gateway-api)
  is the reference for CRD design prior art.

### Controllers and reconcilers

- Controllers are thin: `Reconcile` delegates to the `KagentReconciler`
  interface. Business logic lives in the `reconciler` package.
- The standard reconcile loop:
  1. `Get` the resource; on `apierrors.IsNotFound`, clean up side effects and
     return nil.
  2. Reconcile desired state.
  3. Update status (only writing when conditions or observedGeneration
     actually changed, to avoid hot-looping).
  4. Return the error to trigger exponential-backoff requeue.
- Controllers **MUST** be eventually consistent: fire-and-report, never
  poll or block inside a reconcile waiting for external state. Use
  `RequeueAfter` for time-based re-checks.
- Errors during reconciliation **MUST** be returned (to requeue), not merely
  logged — a logged-and-dropped error means the controller never retries.
- Ownership rules:
  - A controller **MUST NOT** modify (patch, annotate, update status of,
    delete) resources it does not own — including resources owned by other
    controllers (e.g. kmcp) and user-created resources such as secrets.
  - Secondary resources the controller creates **MUST** carry
    `OwnerReferences` (`controllerutil.SetControllerReference`) so they are
    garbage-collected with the parent. Use finalizers when cleanup involves
    anything beyond owned K8s objects; adding a finalizer returns
    `ctrl.Result{Requeue: true}`.
- All controllers set `NeedLeaderElection: new(true)`.

### Translators

- The translator is a **pure function** of cluster state: given inputs, it
  deterministically produces outputs. It **MAY** read K8s resources referenced
  by the API objects it is translating (e.g. resolving a `ModelConfig` or
  `Secret` a spec points to), but it **MUST NOT** write or mutate K8s
  resources, make other network calls, or have knowledge of backends/runtimes.
  Side effects and async logic belong in the reconciler (or a dedicated
  controller).
- Every behavioral change to a translator **SHOULD** come with golden tests
  (`UPDATE_GOLDEN=true go test ...` to regenerate).

### Database

- Prefer real, indexed columns for fields used as query predicates. Querying
  into JSON columns **MAY** be used when pragmatic, but if a JSON field becomes
  a common filter or grows fragile query logic, promote it to a proper column.

### Concurrency

- The core long-running routines are owned by controller-runtime, the HTTP
  server, and the A2A framework. Long-running background work implements
  `manager.Runnable` rather than being spawned ad hoc.
- Goroutines in business logic **MAY** be used when there's a real need
  (fan-out, timeouts, parallel I/O), but they **MUST** be bounded, tied to a
  `context.Context` for cancellation, and must not leak.

### Testing

- Framework: stdlib `testing` + `testify` (`require` for fatal assertions,
  `assert` for non-fatal). No Ginkgo. Test files use external test packages
  (`package foo_test`) unless testing unexported internals.
- Tests are table-driven with `t.Run`, using `name`/`input`/`want`/`wantErr`
  fields.
- Helpers call `t.Helper()` first and register cleanup with `t.Cleanup`.
- Mocks are small hand-written structs — no generated mocks. K8s is faked with
  `fake.NewClientBuilder()`; Postgres uses testcontainers (skipped under
  `testing.Short()`).
- Integration/E2E tests **MUST** exercise the system through its own APIs, the
  way a real client would — not by pre-seeding or asserting via raw SQL, which
  sidesteps the code being validated.
- Assertions must be meaningful: assert on values that prove behavior, not
  strings echoed from the request.

---

## Python

*TODO — to be drafted. Known review themes to incorporate:*

- Tools should be general-purpose, not hyper-specific sub-commands.
- Validate configuration at `__init__`/config-load time (fail fast), not lazily.
- No `except Exception: pass` — never swallow exceptions silently; log at minimum.
- Reuse upstream library types (e.g. ADK `BaseTool`) before reimplementing.
- Anything exported from a package should belong to that package's domain.

---

## TypeScript

*TODO — to be drafted. Known baseline: no `any`; UI code only (no backend logic).*

---

## Helm / YAML

*TODO — to be drafted. Known review themes to incorporate:*

- Create resources conditionally (e.g. only create a secret when the value is set).
- Prefer example values files over proliferating `make install-*` targets.
- Sub-charts don't need explicit `enabled` flags.
- Chart upgrades must be backwards-compatible.
