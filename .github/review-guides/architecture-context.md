# Architecture Context for Reviews

Load when reviewing PRs that touch multiple subsystems, modify controllers, or change CRD types.

**See also:** [impact-analysis.md](impact-analysis.md), [language-checklists.md](language-checklists.md)

---

## High-level architecture

```
UI (Next.js) -> Controller HTTP Server (Go) -> A2A proxy -> Agent Pod (Python/Go ADK)
  -> MCP Tool Servers -> LLM Provider -> back to UI via SSE streaming
```

## Key subsystem boundaries

| Subsystem | Language | Root path |
|-----------|----------|-----------|
| CRD Types & API | Go | `go/api/` |
| Controllers | Go | `go/core/internal/controller/` |
| HTTP Server | Go | `go/core/internal/httpserver/` |
| Database Layer | Go | `go/core/internal/database/` |
| A2A Protocol | Go | `go/core/internal/a2a/` |
| MCP Integration | Go | `go/core/internal/mcp/` |
| CLI | Go | `go/core/cli/` |
| Go ADK | Go | `go/adk/` |
| Python ADK | Python | `python/packages/kagent-adk/` |
| Web UI | TypeScript | `ui/src/` |
| Helm Charts | YAML | `helm/` |

## Critical dependency directions

Flag violations of these dependency rules:

```
# Allowed direction (arrow = "may depend on"). Reverse is forbidden.
go/core/  -> go/api/       (core may use api types, NOT the reverse)
go/adk/   -> go/api/       (adk may use api types, NOT the reverse)
go/core/internal/controller/  -> go/core/internal/database/

# Forbidden imports
go/api/   must NOT import go/core/ or go/adk/
ui/       must NOT import go/ or python/ directly
```

Full dependency map: see [code-tree.md](../../docs/agents/code-tree.md#key-module-dependencies).

## Controller patterns

- **Shared reconciler**: All controllers share `kagentReconciler` — changes affect all CRD types
- **Translator pattern**: CRD spec → K8s resources via translators. PRs must update translator when adding CRD fields
- **Database-level concurrency**: Atomic upserts, no application-level locks. Do NOT introduce mutexes
- **Idempotent reconciliation**: Each loop iteration must be safe to retry
- **Network I/O outside transactions**: Long-running operations must not hold database locks

## CRD type alignment

When Go CRD types mirror Python ADK types (e.g., `AgentSpec` → `types.py`):

- Add cross-reference comments in both languages
- Go types are the source of truth
- Flag changes to one side without corresponding changes to the other
- Both serialize to JSON via `config.json` — field names must match

## Backward compatibility

For CRD/API changes:

- New fields must have safe defaults (zero-value must not break existing agents)
- What happens when an old controller reads a new CRD? Migration path must be explicit
- `v1alpha2` allows breaking changes, but prefer backward-compatible additions
- Database schema changes require migration logic in `go/core/internal/database/`

## Cardinality changes

When a value changes from single to list (or vice versa):

1. Use `--callers` and `--rdeps` to find all consumers
2. Check for single-value assumptions in translators, HTTP handlers, and UI
3. Verify database model handles the change
4. Verify Helm templates handle the change

## Type hierarchies

Changes to base types affect all consumers. Key hierarchies in [code-tree.md](../../docs/agents/code-tree.md#key-type-hierarchies).
