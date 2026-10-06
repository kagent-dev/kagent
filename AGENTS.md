# Kagent Development Guide

This file defines the repository-wide rules for agents working on kagent. Read the code in scope before changing it. For detailed development and CI workflows, use the `kagent-dev` skill. See [STYLE.md](STYLE.md) for language-specific conventions.

`AGENTS.md` is the canonical repository instruction filename. Before editing a
file, read every applicable nested `AGENTS.md` along its directory path, even when
starting from the repository root. Nested guidance adds directory-specific rules;
the shared architecture and component boundaries below still apply.

## 1. Current Architecture

Kagent is a Kubernetes-native control plane for defining, running, and invoking AI agents.

- `Agent`, `Harness`, and `AgentTemplate` are `api.kagent.dev/v1alpha3` Kubernetes APIs. Agent composes inline or referenced behavior and runtime configuration. AgentTemplate is reusable behavior; Harness selects how it runs.
- `Session` is PostgreSQL-backed control-plane state exposed through gRPC. It is not a Kubernetes resource.
- Upstream A2A owns task, interaction, streaming, and history semantics. Do not create parallel session or task models.
- Substrate Actors are the compute backend. Durable directories own private runtime state that must survive actor replacement.
- Harness compilers translate resolved templates into backend inputs. Keep compilation separate from applying those inputs.
- The public API describes agent behavior, not infrastructure mechanics. Do not expose Kubernetes scheduling, service accounts, arbitrary containers, channels, profiles, or generic extension maps.

The release-blocking harnesses are kagent, Codex, and Claude. Prefer clean-install behavior over compatibility with legacy session-backed agents unless compatibility is explicitly required.

## 2. Sources of Truth

- Current architecture is documented in [docs/architecture](docs/architecture).
- Development workflows and current architecture notes are in [.claude/skills/kagent-dev/SKILL.md](.claude/skills/kagent-dev/SKILL.md).
- General and language-specific conventions are in [STYLE.md](STYLE.md).
- Generated code is never the source of truth. Change the API, protobuf, SQL, or schema source and regenerate its outputs.

When documentation and implementation disagree, verify the intended state against the current task requirements and code rather than preserving obsolete behavior.

## 3. Code Structure — Make Wrong Code Hard to Write

The codebase is organized around three ideas that keep it maintainable as it grows:

**Semantic functions** are small, pure operations with clear inputs and outputs. They do one thing, are named for *what they compute* rather than where they are called, and do not touch I/O or global state. Keep them minimal and easy to unit-test. If a function grows beyond its name, it is absorbing pragmatic concerns; split it.

**Pragmatic functions** are the glue that wires semantic functions to the real world: HTTP and gRPC handlers, workflow orchestration, pool management, error recovery, and backend dispatch. These live in a few well-known places and should document gotchas rather than obvious behavior. When pragmatic logic creeps into a semantic function, extract it.

**Data models make wrong states unrepresentable.** Use the type system and database constraints to enforce invariants instead of repeatedly checking them at runtime. When adding a struct or type, ask: “Can a caller construct an instance of this that does not make sense?” If yes, tighten the model until they cannot.

Avoid speculative abstractions. Add an interface when there is a real boundary or multiple implementations, not merely to wrap one concrete type.

## 4. Component Boundaries — Each Layer Has One Job

Every component has a single responsibility. If code reaches into another component's internals, the behavior is probably in the wrong place.

- **Transport handlers** convert wire formats to domain types and call one service or workflow operation. They do not orchestrate, hold locks, or know backend details.
- **Protobuf request validation** belongs in source `.proto` annotations and runs through the shared gRPC Protovalidate interceptor. Authorization and checks requiring external state remain in the owning service or workflow. See [proto/AGENTS.md](proto/AGENTS.md) for validation and generation rules.
- **Services and workflows** orchestrate operations end-to-end. They know the order of operations, but delegate each step to the component that owns it.
- **Harness compilers** resolve agent configuration into explicit build inputs. They do not apply resources or perform transport work.
- **Harness adapters and Substrate clients** own runtime-specific creation and lifecycle details. Backend decisions stay behind this boundary.
- **The store** persists state and enforces transactional invariants. It does not launch actors, fetch assets, or register proxies.

**Operations are atomic from the caller's perspective.** Database-only operations use transactions. Workflows that cross database and network boundaries use durable phases, idempotent retries, and compensating cleanup so partial work can be safely resumed or removed. Never hold a database transaction or lock across a network call.

**Persist transitions atomically.** When one logical transition changes task state and history, compute it before persistence and commit it through one store transaction. Transport code must not perform preparatory writes. Reject malformed durable data rather than silently omitting it.

**Internal mechanics are not API.** Locks, accounting counters, query sequencing, and cloned dependencies stay hidden from callers. An implementation change should not force callers to change.

**Behavior lives where the knowledge is.** Do not move behavior sideways into a wrapper; push it down to the component that understands the domain.

## 5. Repository Map

| Path | Responsibility |
| --- | --- |
| `go/api/v1alpha3` | Current Kubernetes API types |
| [go/core/internal/grpcserver](go/core/internal/grpcserver/AGENTS.md) | gRPC transport |
| `go/core/internal/service` | Control-plane services and workflows |
| [go/core/internal/database](go/core/internal/database/AGENTS.md) | PostgreSQL queries and persistence |
| `go/core/internal/{a2agateway,controller,egress,mcp,substrate,translator}` | API v2 execution and A2A gateway |
| `go/adk` | Go agent development kit |
| `python/packages` | Python agent packages and ADK |
| [proto](proto/AGENTS.md) | gRPC API definitions |
| [ui](ui/AGENTS.md) | Web UI |
| `helm` | Kubernetes packaging |

Do not add new work to legacy API versions unless the change is explicitly a compatibility fix.

## 6. Change Workflow

Keep all planning documents, implementation trackers, and planning notes in the
gitignored `.plans/` directory at the repository root. Create it when needed;
do not use `docs/plans/` or commit plans. Merge lasting architecture decisions
and design rationale into `docs/architecture/` as part of the relevant change.
Tracked documentation must stand on its own without links to local plans.

1. Trace the existing behavior and all callers before editing.
2. Change the narrowest source of truth that fixes the behavior for every caller.
3. Add focused unit coverage for semantic logic and E2E coverage for API, persistence, lifecycle, or runtime behavior.
4. Regenerate affected artifacts. API and protobuf changes require their repository generation targets. SQL changes require the PostgreSQL store tests, including the inline-statement prepare check.
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
| Build and push images | `make build` |
| Go unit tests | `make -C go test` |
| Go E2E tests | `make -C go e2e` |
| Go lint | `make -C go lint` |
| Generate Go artifacts | `make -C go generate` |
| Create a Kind cluster | `make create-kind-cluster` |
| Install into Kind | `make helm-install` |

## 8. Git and Review

- Use Conventional Commits: `feat:`, `fix:`, `refactor:`, `test:`, `docs:`, `chore:`, `perf:`, or `ci:`.
- Sign off commits with `git commit -s`.
- Do not commit or push unless asked.
- Do not open a pull request, including a draft, without explicit approval.
- Keep PRs focused. Explain non-obvious invariants and operational tradeoffs, not line-by-line implementation details.

## 9. References

- [STYLE.md](STYLE.md)
- [DEVELOPMENT.md](DEVELOPMENT.md)
- [CONTRIBUTING.md](CONTRIBUTING.md)
- [docs/architecture](docs/architecture)
