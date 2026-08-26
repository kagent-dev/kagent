# CLAUDE.md — Kagent Development Guide

This file defines the repository-wide rules for agents working on kagent. Read the code in scope before changing it. For detailed development and CI workflows, use the `kagent-dev` skill. See [STYLE.md](STYLE.md) for language-specific conventions.

## 1. Current Architecture

Kagent is a Kubernetes-native control plane for defining, running, and invoking AI agents.

- `Harness` and `AgentTemplate` are `kagent.dev/v1alpha3` Kubernetes APIs. A harness describes how to compile a template into runnable inputs; a template describes the agent users want.
- `AgentInstance` is PostgreSQL-backed control-plane state exposed through gRPC. It is not a Kubernetes resource.
- Upstream A2A owns task, interaction, streaming, and history semantics. Do not create parallel session or task models.
- Substrate Actors are the compute backend. Durable directories own private runtime state that must survive actor replacement.
- Harness compilers translate resolved templates into backend inputs. Keep compilation separate from applying those inputs.
- The public API describes agent behavior, not infrastructure mechanics. Do not expose Kubernetes scheduling, service accounts, arbitrary containers, channels, profiles, or generic extension maps.

The release-blocking harnesses are kagent, Codex, and Claude. Prefer clean-install behavior over compatibility with legacy session-backed agents unless compatibility is explicitly required.

## 2. Sources of Truth

- The API v2 execution plan is [docs/plans/api-v2-execution-plan.md](docs/plans/api-v2-execution-plan.md).
- Development workflows and current architecture notes are in [.claude/skills/kagent-dev/SKILL.md](.claude/skills/kagent-dev/SKILL.md).
- General and language-specific conventions are in [STYLE.md](STYLE.md).
- Generated code is never the source of truth. Change the API, protobuf, SQL, or schema source and regenerate its outputs.

When documentation and implementation disagree, verify the intended state in the execution plan and current code rather than preserving obsolete behavior.

## 3. Code Structure — Make Wrong Code Hard to Write

The codebase is organized around three ideas that keep it maintainable as it grows:

**Semantic functions** are small, pure operations with clear inputs and outputs. They do one thing, are named for *what they compute* rather than where they are called, and do not touch I/O or global state. Keep them minimal and easy to unit-test. If a function grows beyond its name, it is absorbing pragmatic concerns; split it.

**Pragmatic functions** are the glue that wires semantic functions to the real world: HTTP and gRPC handlers, workflow orchestration, pool management, error recovery, and backend dispatch. These live in a few well-known places and should document gotchas rather than obvious behavior. When pragmatic logic creeps into a semantic function, extract it.

**Data models make wrong states unrepresentable.** Use the type system and database constraints to enforce invariants instead of repeatedly checking them at runtime. When adding a struct or type, ask: “Can a caller construct an instance of this that does not make sense?” If yes, tighten the model until they cannot.

Avoid speculative abstractions. Add an interface when there is a real boundary or multiple implementations, not merely to wrap one concrete type.

## 4. Component Boundaries — Each Layer Has One Job

Every component has a single responsibility. If code reaches into another component's internals, the behavior is probably in the wrong place.

- **Transport handlers** convert wire formats to domain types and call one service or workflow operation. They do not orchestrate, hold locks, or know backend details.
- **Protobuf request validation** is declared in source `.proto` files with `buf.validate` annotations and enforced by the shared gRPC Protovalidate interceptor before handlers run. Use standard rules first, CEL for request-intrinsic cross-field or domain rules, and reusable predefined rules only when needed across schemas. The annotations live in generated Go descriptors; there are no generated validator files. Authorization and checks requiring database, Kubernetes, or network state remain in the owning service or workflow.
- **Services and workflows** orchestrate operations end-to-end. They know the order of operations, but delegate each step to the component that owns it.
- **Harness compilers** resolve agent configuration into explicit build inputs. They do not apply resources or perform transport work.
- **Harness adapters and Substrate clients** own runtime-specific creation and lifecycle details. Backend decisions stay behind this boundary.
- **The store** persists state and enforces transactional invariants. It does not launch actors, fetch assets, or register proxies.

**Operations are atomic from the caller's perspective.** Database-only operations use transactions. Workflows that cross database and network boundaries use durable phases, idempotent retries, and compensating cleanup so partial work can be safely resumed or removed. Never hold a database transaction or lock across a network call.

**Internal mechanics are not API.** Locks, accounting counters, query sequencing, and cloned dependencies stay hidden from callers. An implementation change should not force callers to change.

**Behavior lives where the knowledge is.** Do not move behavior sideways into a wrapper; push it down to the component that understands the domain.

## 5. Repository Map

| Path | Responsibility |
| --- | --- |
| `go/api/v1alpha3` | Current Kubernetes API types |
| `go/core/internal/grpcserver` | gRPC transport |
| `go/core/internal/service` | Control-plane services and workflows |
| `go/core/internal/database` | PostgreSQL queries and persistence |
| `go/core/v2` | API v2 execution and A2A gateway |
| `go/adk` | Go agent development kit |
| `python/packages` | Python agent packages and ADK |
| `proto` | gRPC API definitions |
| `ui` | Web UI |
| `helm` | Kubernetes packaging |

Do not add new work to legacy API versions unless the change is explicitly a compatibility fix.

## 6. Change Workflow

1. Trace the existing behavior and all callers before editing.
2. Change the narrowest source of truth that fixes the behavior for every caller.
3. Add focused unit coverage for semantic logic and E2E coverage for API, persistence, lifecycle, or runtime behavior.
4. Regenerate affected artifacts. SQL changes require `sqlc generate`; API and protobuf changes require their repository generation targets.
5. Run the smallest relevant checks first, then the broader lint and test targets appropriate to the change.

Preserve unrelated work in a dirty tree. Do not hand-edit generated outputs, add dependencies without need, or introduce compatibility behavior speculatively.

## 7. Testing and Validation

- Unit-test new semantic behavior and failure paths.
- Use table-driven Go tests where they make cases clearer; do not force the pattern onto a single case.
- Mock external services in unit tests. Use real integration boundaries in E2E tests.
- Add E2E coverage for new CRD fields, public endpoints, persistence workflows, and runtime lifecycle behavior.
- Test retries and partial failures for workflows that span PostgreSQL and Substrate.
- Run formatting, generation checks, lint, and relevant tests before committing.

Common commands:

| Task | Command |
| --- | --- |
| Build | `make build` |
| Unit tests | `make test` |
| Go E2E tests | `make -C go e2e` |
| Go lint | `make -C go lint` |
| Generate Go artifacts | `make -C go generate` |
| Create a Kind cluster | `make create-kind-cluster` |
| Install into Kind | `make helm-install` |

## 8. Git and Review

- Use Conventional Commits: `feat:`, `fix:`, `refactor:`, `test:`, `docs:`, `chore:`, `perf:`, or `ci:`.
- Sign off commits with `git commit -s`.
- Do not commit or push unless asked.
- Keep PRs focused. Explain non-obvious invariants and operational tradeoffs, not line-by-line implementation details.

## 9. References

- [STYLE.md](STYLE.md)
- [DEVELOPMENT.md](DEVELOPMENT.md)
- [CONTRIBUTING.md](CONTRIBUTING.md)
- [docs/architecture](docs/architecture)
- [docs/plans/api-v2-execution-plan.md](docs/plans/api-v2-execution-plan.md)
