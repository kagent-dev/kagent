# Hatchet Reuse Feasibility for Kagent Workflow Engine

**Date:** 2026-03-10
**Status:** Research
**Subject:** Can [Hatchet](https://github.com/hatchet-dev/hatchet) be reused as the workflow engine for kagent's declarative workflow builder/executor?

---

## 1. Architecture

Hatchet is a **standalone orchestration platform**, not an embeddable library. It consists of three core services:

| Component | Role |
|-----------|------|
| **Engine** | Orchestration layer: evaluates DAG dependencies, enforces concurrency/rate limits, schedules tasks, persists state. Communicates with workers via bidirectional gRPC. |
| **API Server** | HTTP API for triggering workflows, querying state, managing resources. Powers the dashboard UI. |
| **Workers** | User-operated long-running processes that connect to the engine via gRPC, execute task code, report results. |

**Runtime dependencies:**
- **PostgreSQL** (required) — authoritative store for workflow definitions, execution state, queue management. Uses range/hash partitioning, triggers, and buffered writes for throughput.
- **RabbitMQ** (optional) — for high-throughput inter-service messaging. Removed as a requirement in v1; PGMQ (PostgreSQL-based MQ) is the default.
- **No Temporal dependency** — Hatchet v0 (the original `hatchet-workflows` repo, now archived) used Temporal as its execution backend. Hatchet v1 (current, the main `hatchet-dev/hatchet` repo) is a **complete rewrite with its own engine** built entirely on PostgreSQL. Temporal is no longer used.

**Deployment models:**
- **Hatchet Cloud** — managed SaaS
- **Hatchet Lite** — single Docker image (dev/testing)
- **Docker Compose** — multi-container with separate Postgres
- **Kubernetes via Helm** — production charts (`hatchet-stack` and `hatchet-ha`)

## 2. Language / SDK

**Core language:** Go (the engine, API server, CLI are all Go).

**SDKs:**
| Language | Maturity |
|----------|----------|
| Go | Production — `github.com/hatchet-dev/hatchet/sdks/go` (new v1 SDK, old generics-based SDK deprecated) |
| Python | Production — `hatchet-sdk` on PyPI |
| TypeScript | Production |
| Ruby | Early/experimental |

The Go SDK is a **client SDK** for defining workflows and running workers that connect to the Hatchet engine over gRPC. It is **not** a library for embedding the engine itself.

## 3. License

**MIT License** — fully permissive open source. Compatible with kagent's Apache 2.0 license. No BUSL or source-available restrictions. Can be freely embedded, forked, and redistributed.

## 4. Workflow Definition Model

**Code-first, not declarative YAML/JSON.** Workflows are defined programmatically using SDK-specific constructs:

```go
// Go example
wf := hatchet.Workflow(hatchet.WorkflowOpts{Name: "my-dag"})

step1 := wf.Task(hatchet.TaskOpts{Name: "step1"}, func(ctx context.Context, input Input) (*Output, error) {
    // ...
})

step2 := wf.Task(hatchet.TaskOpts{Name: "step2", Parents: []hatchet.Task{step1}}, func(ctx context.Context, input Input) (*Output, error) {
    result := ctx.TaskOutput(step1)
    // ...
})
```

**DAG support:** Yes — tasks declare `Parents` (dependencies). Hatchet builds a DAG, manages execution order, and routes parent outputs to child inputs automatically.

**Conditions:** Supports conditional logic based on parent output (added in v1).

**Historical note:** The archived `hatchet-workflows` repo had a **YAML-based declarative syntax** inspired by GitHub Actions (with `on:` triggers and `jobs:` containing `steps:`). This was abandoned when Hatchet moved to its own engine and code-first approach.

**No native YAML/JSON workflow definition exists in current Hatchet.** To use Hatchet with a declarative DSL, kagent would need to build a translation layer that converts YAML/JSON definitions into Hatchet SDK calls — essentially the same work as building a declarative layer over Temporal.

## 5. Durable Execution

Hatchet v1 implements its **own durable execution engine** (no Temporal under the hood).

**Durability model:**
- Task state transitions are persisted transactionally in PostgreSQL
- At-least-once execution semantics (idempotent tasks recommended)
- On failure, workflows resume from the last successful checkpoint
- Intermediate results are cached and replayed on retry
- Event log per workflow for observability

**Compared to Temporal's replay-based model:**
- Temporal replays the entire workflow function deterministically from an event history
- Hatchet uses a DAG-based approach where each task is an independent unit; on retry, completed tasks are skipped based on their persisted completion status
- Hatchet's model is simpler (no determinism constraints on workflow code) but less flexible for complex control flow patterns

## 6. Kubernetes Integration

**Helm charts available:**
- `hatchet/hatchet-stack` — standard deployment
- `hatchet/hatchet-ha` — high-availability deployment
- Glasskube support as alternative installer

**No CRD support.** Hatchet does not define Kubernetes CRDs. Workflows are registered by workers connecting to the engine via gRPC, not via Kubernetes resources. There is no Kubernetes operator.

**Integration with a K8s operator:** Would require kagent to either:
1. Deploy Hatchet as a sidecar/dependency and bridge CRDs to Hatchet API calls, or
2. Run Hatchet as an infrastructure dependency (separate Helm release) and have the kagent controller communicate with it via HTTP/gRPC API

## 7. Agent / LLM Support

Hatchet explicitly positions itself for **agentic AI pipelines**:
- Built-in support for long-running agent workflows
- Tool calls modeled as tasks with timeout/retry semantics
- Human-in-the-loop via eventing/signaling
- Streaming response support
- Child workflow spawning for dynamic agent behavior
- Conversation state management via durable execution

However, this support is at the **infrastructure/orchestration** level. Hatchet does not include LLM client libraries, prompt management, or MCP integration. It provides the execution substrate, not the AI agent framework itself.

## 8. Embedding Feasibility

**Hatchet cannot be embedded as a Go library.** Key findings:

| Aspect | Status |
|--------|--------|
| Use as in-process library | **Not supported** — engine is a separate service |
| Use worker SDK only | **Possible** — Go worker SDK can run in-process alongside other code |
| Engine as sidecar | **Possible** — Hatchet Lite runs as single container |
| Extract just the DAG engine | **Not feasible** — engine is tightly coupled to PostgreSQL, gRPC server, API layer |

The Hatchet Go SDK (`github.com/hatchet-dev/hatchet/sdks/go`) is a **client library** that:
- Registers workflows with a remote Hatchet engine
- Starts worker processes that receive tasks via gRPC
- Reports task results back to the engine

You cannot instantiate the Hatchet engine programmatically within another Go binary. It must run as a separate process/container.

**Implications for kagent:** Adopting Hatchet means adding a significant infrastructure dependency — a separate Hatchet engine deployment plus PostgreSQL (Hatchet's own, separate from kagent's DB). This is architecturally similar to deploying Temporal as a dependency.

## 9. Community and Maturity

| Metric | Value |
|--------|-------|
| GitHub stars | ~6,700 |
| Contributors | ~30 |
| Forks | ~314 |
| Language | Go |
| Created | December 2023 |
| License | MIT |
| Latest release | v0.79.32 (2026-03-09) — very active, daily releases |
| v1 engine | GA since May 2025 (v0 EOL September 2025) |
| Throughput tested | 5,000 tasks/sec, 1B+ tasks/month in production |
| Release cadence | Multiple releases per week, often daily |

**Maturity assessment:** Active development, frequent releases, but relatively small team (~4 known hires). The project is past its initial phase (v1 rewrite complete) and has production users processing significant volumes. However, the version numbering (v0.79.x) and the fact that v1 engine only went GA in mid-2025 indicate it is still maturing.

## 10. Comparison: Hatchet vs. Building on Temporal Directly

| Dimension | Hatchet | Temporal (direct) |
|-----------|---------|-------------------|
| **Infrastructure footprint** | Engine + PostgreSQL (+ optional RabbitMQ) | Temporal server + PostgreSQL/Cassandra/MySQL (+ optional Elasticsearch) |
| **Deployment complexity** | Similar — separate service + database | Similar — separate service + database |
| **Embedding in kagent binary** | Not possible | Not possible (Temporal server is also separate) |
| **Go SDK maturity** | Good but newer (v1 SDK since 2025) | Very mature (years of production use) |
| **Declarative workflow support** | None — code-first only | None — code-first only |
| **DAG support** | Native DAG with parent dependencies | Must be built on top of activities/child workflows |
| **Durable execution model** | Task-level persistence, no determinism constraints | Replay-based, requires deterministic workflow code |
| **AI/Agent workflows** | Explicitly targeted | General-purpose, no AI-specific features |
| **Community size** | ~6.7K stars, ~30 contributors | ~33K+ stars, 200+ contributors |
| **Production track record** | ~1 year of v1 in production | 5+ years of production use at scale |
| **License** | MIT | MIT |
| **Kagent existing integration** | None — would be new dependency | Already used by kagent (per codebase context) |

### What kagent gains with Hatchet over Temporal:
- Native DAG-based workflow model (aligns well with declarative workflow DAGs)
- Simpler durability model (no determinism constraints)
- Explicit AI/agentic workflow positioning and features
- Simpler infrastructure (PostgreSQL only, no Cassandra/Elasticsearch)
- Sub-25ms task dispatch for hot workers

### What kagent loses with Hatchet over Temporal:
- **Existing Temporal integration** — kagent already has Temporal; switching adds migration cost
- Smaller community, fewer contributors, shorter production track record
- Less mature Go SDK
- No declarative workflow definition (same gap as Temporal)
- Temporal's more powerful workflow model (sagas, signals, queries, long-running workflows with complex control flow)
- Temporal's broader ecosystem and battle-tested reliability at massive scale

---

## Recommendation

**Hatchet is not a good fit for embedding as kagent's workflow engine.** Key reasons:

1. **Cannot be embedded** — Hatchet requires running as a separate service with its own PostgreSQL database. This adds the same infrastructure complexity as Temporal, which kagent already uses.

2. **No declarative workflow support** — Hatchet uses a code-first model. Kagent would still need to build a YAML/JSON-to-code translation layer, which is the same core work regardless of whether the backend is Hatchet or Temporal.

3. **Kagent already has Temporal** — Switching from Temporal (established, battle-tested) to Hatchet (newer, smaller community) introduces risk without solving the declarative workflow problem.

4. **Where Hatchet shines doesn't help** — Hatchet's advantages (simpler durability model, native DAGs, AI-focused features) are valuable for greenfield projects but don't justify a migration when Temporal already works.

**Better approach for kagent:** Build a thin declarative DSL layer (YAML/JSON CRD) that translates to Temporal workflows/activities. This leverages kagent's existing Temporal integration, avoids adding a new infrastructure dependency, and solves the actual problem (declarative workflow definition) directly.

**Hatchet could be reconsidered if:**
- Kagent drops Temporal entirely and needs a simpler PostgreSQL-only backend
- Hatchet adds native declarative/YAML workflow definitions
- Hatchet provides an embeddable engine mode (in-process, no separate server)
