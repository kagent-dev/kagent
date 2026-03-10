# PROMPT: NATS Activity Feed

## Objective

Build a read-only activity feed that subscribes to NATS `agent.>` and streams agent events to the browser via SSE. Single Go binary at `go/plugins/nats-activity-feed/` with embedded HTML UI.

## Key Requirements

1. Go binary following kanban-mcp plugin pattern (`go/plugins/kanban-mcp/`)
2. Subscribe to NATS wildcard `agent.>`, parse `StreamEvent` from `go/adk/pkg/streaming/types.go`
3. Extract agent name + session ID from NATS subject (`agent.{name}.{session}.stream`)
4. SSE hub with ring buffer (last 100 events) — adapt `go/plugins/kanban-mcp/internal/sse/hub.go`
5. Embedded single-file HTML SPA — live scrolling feed, color-coded by event type, auto-reconnect
6. Config: `--nats-addr` (default `nats://localhost:4222`), `--addr` (default `:8090`), `--buffer-size`, `--subject`
7. Dockerfile + Helm chart in `helm/tools/nats-activity-feed/`

## Acceptance Criteria

- **Given** agents publish to NATS, **When** user opens browser, **Then** live event feed appears
- **Given** new browser connects, **Then** ring buffer contents sent as initial burst
- **Given** NATS drops, **Then** auto-reconnects without user action
- **Given** no activity, **Then** UI shows "Waiting for activity..."
- **Given** multiple agents active, **Then** events interleaved chronologically

## Reference

- Design: `specs/nats-activity-feed/design.md`
- Plan: `specs/nats-activity-feed/plan.md` (6 steps, follow in order)
- Pattern to follow: `go/plugins/kanban-mcp/` (SSE hub, embedded HTML, config, Dockerfile)
- Event types: `go/adk/pkg/streaming/types.go` (import, don't duplicate)
