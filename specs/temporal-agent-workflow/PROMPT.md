# Temporal Agent Workflow Executor

## Objective

Integrate Temporal as a durable workflow executor for kagent's Go ADK. Replace the synchronous `Agent.Run()` call in `go/adk/pkg/a2a/executor.go` with Temporal workflows providing per-turn activity granularity, real-time NATS streaming, HITL signals, and child workflows for A2A multi-agent orchestration. Feature is per-agent via CRD spec (`spec.temporal.enabled`).

## Key Requirements

- Go ADK only (Python out of scope)
- Each LLM turn = separate Temporal activity; each tool call = separate activity
- Real-time token streaming via NATS pub/sub (fire-and-forget, embedded in Helm)
- HITL approval via Temporal signals + `POST /api/sessions/{id}/approve` endpoint
- A2A multi-agent calls = child workflows on per-agent task queues (`agent-{name}`)
- Per-agent CRD spec control: `spec.temporal.enabled`, `workflowTimeout` (default 48h), `retryPolicy`
- Self-hosted Temporal: SQLite (dev) / PostgreSQL (prod), switchable via Helm values
- Temporal worker in-process alongside A2A server in agent pod
- Temporal UI exposed as kagent MCP plugin in sidebar
- Existing synchronous path unchanged when Temporal not enabled

## Acceptance Criteria

- **Given** `spec.temporal.enabled: true` on Agent CRD, **When** A2A message sent, **Then** executes as Temporal workflow with per-turn activities on queue `agent-{name}`
- **Given** worker pod crashes mid-execution, **When** pod restarts, **Then** workflow resumes from last completed activity
- **Given** LLM tokens generated during activity, **When** streaming, **Then** tokens published to NATS `agent.{name}.{session}.stream` and forwarded via SSE
- **Given** HITL required, **When** `POST /api/sessions/{id}/approve` sent, **Then** workflow receives signal and resumes
- **Given** agent A invokes agent B, **When** both Temporal-enabled, **Then** B runs as child workflow linked to A
- **Given** `spec.temporal` absent, **When** A2A message sent, **Then** existing `Agent.Run()` path used unchanged
- **Given** Helm `temporal.enabled: true`, **When** installed, **Then** Temporal server + NATS + Temporal UI plugin deployed

## Reference

Full spec at `specs/temporal-agent-workflow/`:
- `design.md` -- architecture, components, data models, error handling, diagrams
- `plan.md` -- 11-step implementation checklist
- `requirements.md` -- 14 Q&A design decisions
- `research/` -- kagent execution model and Temporal SDK analysis
