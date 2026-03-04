# Go Workspace Restructuring — Decision Document

This document captures the key decisions needed to split the current `go/` module into a Go workspace with three modules: `kagent-core`, `kagent`, and `kagent-adk`.

**Instructions:** Fill in the "Decision" section for each question. Add any notes or constraints in the "Notes" field.

---

## 1. API/CRD Types Ownership

The CRD types in `go/api/v1alpha1/` and `go/api/v1alpha2/` are used by both the controller (internal infrastructure) and the client library (external consumers). Which module should own these types?

**Options:**
- **A)** Put them in `kagent-core` — `kagent` (client lib) would depend on `kagent-core`
- **B)** Put them in `kagent` (external-facing module) — `kagent-core` would depend on `kagent`
- **C)** Create a 4th shared types module (e.g., `kagent-api`) — both `kagent-core` and `kagent` depend on it

**Decision:**

C

**Notes:**

---

## 2. `pkg/` Package Breakdown

The current `go/pkg/` contains packages that serve different audiences. For each, indicate which module it belongs to.

| Package | Description | Module (`kagent-core`, `kagent`, or `kagent-adk`) |
|---------|-------------|---------------------------------------------------|
| `pkg/client` | REST HTTP client SDK for the kagent API | kagent |
| `pkg/database` | GORM-based database abstraction (sessions, agents, tasks, etc.) | kagent-core |
| `pkg/app` | Application bootstrap & configuration management | kagent-core |
| `pkg/auth` | Authentication/authorization interfaces and middleware | kagent-core |
| `pkg/env` | Environment variable registry with typed accessors | kagent-core |
| `pkg/translator` | ADK API translation types | kagent-core |
| `pkg/mcp_translator` | MCP server translation plugin interface | kagent-core |
| `pkg/adk` | Go ADK type definitions | kagent-core |
| `pkg/utils` | Common utility functions | kagent-core |

**Notes:**

---

## 3. Binary Entry Points

Where do the binaries live?

### 3a. Controller binary (`cmd/controller/main.go`)

The controller binary orchestrates K8s controllers + httpserver + database.

**Options:**
- **A)** Keep it in `kagent-core` (since it imports all the internal infrastructure)
- **B)** Put it in a separate top-level `go/cmd/` directory outside any module, importing from modules
- **C)** Other

**Decision:**

A

### 3b. CLI (`cli/`)

The CLI is a TUI client for interacting with kagent.

**Options:**
- **A)** Put it in `kagent` (since it's a consumer of the client library)
- **B)** Put it in `kagent-core`
- **C)** Keep it in a separate top-level location
- **D)** Other

**Decision:**

B

**Notes:**

---

## 4. Directory Layout

What directory names should be used within the repo?

**Options:**
- **A)** `go/kagent-core/`, `go/kagent/`, `go/kagent-adk/`
- **B)** `go/core/`, `go/client/`, `go/adk/`
- **C)** Top-level: `kagent-core/`, `kagent/`, `kagent-adk/` (outside `go/`)
- **D)** Other: _______________

**Decision:**

B

**Notes:**

---

## 5. `kagent-adk` Dependencies

Currently `contrib/go-adk` is completely independent of the main `go/` module. Should the new `kagent-adk` module:

**Options:**
- **A)** Stay independent — just move it physically into the workspace, no new cross-module deps
- **B)** Start depending on `kagent-core` for shared types/utilities
- **C)** Depend on `kagent` for the client library types
- **D)** Other

**Decision:**

B + C

**Notes:**

---

## 6. Module Paths

What should the Go module paths be?

**Options:**
- **A)** Match directory names under `go/`:
  - `github.com/kagent-dev/kagent/go/kagent-core`
  - `github.com/kagent-dev/kagent/go/kagent`
  - `github.com/kagent-dev/kagent/go/kagent-adk`
- **B)** Shorter paths:
  - `github.com/kagent-dev/kagent/go/core`
  - `github.com/kagent-dev/kagent/go/client`
  - `github.com/kagent-dev/kagent/go/adk`
- **C)** Other: _______________

**Decision:**

B

**Notes:**

---

## 7. Config / CRD Manifests

The `go/config/` directory has CRD YAML manifests, RBAC, and sample configs. Where should these live?

**Options:**
- **A)** Stay in `go/config/` at the workspace root level (not inside any module)
- **B)** Move into `kagent-core` alongside the controller code
- **C)** Move to top-level repo `config/` directory
- **D)** Other

**Decision:**

B

**Notes:**

---

## 8. Dependency Direction Summary

Once you've answered the above, confirm the expected dependency graph:

```
kagent-adk  ──?──▶  kagent-core
                        ▲
                        │?
                     kagent
```

- Does `kagent` depend on `kagent-core`? (yes/no): yes
- Does `kagent-adk` depend on `kagent-core`? (yes/no): yes
- Does `kagent-adk` depend on `kagent`? (yes/no): yes
- Any other cross-module dependencies?:

---

## Additional Notes / Constraints

_Add anything else relevant here (e.g., backwards compatibility concerns, timeline, phasing, etc.)_
