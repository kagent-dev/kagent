# Summary

## Artifacts

| File | Description |
|------|-------------|
| `rough-idea.md` | Initial concept |
| `requirements.md` | 14 Q&A pairs covering all design decisions |
| `research/kagent-go-execution.md` | Analysis of current Go ADK execution model |
| `research/temporal-go-sdk.md` | Temporal Go SDK patterns and K8s deployment |
| `design.md` | Full design with architecture, components, data models, acceptance criteria |
| `plan.md` | 11-step implementation plan with checklist |
| `summary.md` | This file |

## Overview

Integrate Temporal as a durable workflow executor for kagent's Go ADK. Each agent execution becomes a Temporal workflow with per-turn LLM activities and per-call tool activities. Real-time streaming via NATS pub/sub. HITL via Temporal signals. Multi-agent A2A via child workflows. Per-agent control via CRD spec.

## Key Decisions

- **Self-hosted Temporal**, SQLite (dev) / PostgreSQL (prod), switchable via Helm
- **In-process worker** alongside A2A server in agent pod
- **Per-turn activity granularity** -- each LLM call and tool call is a separate retryable activity
- **Per-agent task queues** (`agent-{name}`) for isolation
- **NATS fire-and-forget** pub/sub for real-time token streaming (embedded in Helm chart)
- **Per-agent CRD spec** control (`spec.temporal.enabled`), not global env var
- **48h default workflow timeout**, configurable per-agent
- **Temporal UI as MCP plugin** in kagent sidebar
- **HITL and child workflows** required in initial scope

## Next Steps

1. Start implementation at Step 1 (SDK dependencies and package structure)
2. Each step is independently testable and demoable
3. Steps 1-8 are core Go implementation; Step 9 is CRD changes; Steps 10-11 are deployment and E2E
