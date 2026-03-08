# EP-2007: Temporal Workflows Admin UI Plugin

* Status: **Implemented**
* Spec: [specs/temporal-workflows-ui](../specs/temporal-workflows-ui/)

## Background

Custom Temporal workflow administration plugin (`temporal-mcp`) following the kanban-mcp architecture pattern. Stateless Go binary providing MCP tools, REST API, and embedded SPA with SSE live updates for monitoring agent workflow executions.

## Motivation

The stock Temporal UI (SvelteKit) doesn't integrate with kagent's plugin system and can't be proxied through iframes due to CSRF protection and relative module paths. A lightweight custom UI provides workflow visibility within the kagent shell.

### Goals

- 4 MCP tools: list, get, cancel, signal workflows
- REST API for workflow queries
- Embedded vanilla JS SPA with SSE polling (5s interval)
- Stateless — connects directly to Temporal Server via gRPC
- Registered as RemoteMCPServer CRD under Workflows sidebar entry

### Non-Goals

- Full Temporal UI feature parity
- Workflow definition editing
- Temporal namespace management

## Implementation Details

- **Binary:** `go/plugins/temporal-mcp/` — Go server with MCP + REST + SSE + embedded SPA
- **Config:** `TEMPORAL_HOST_PORT`, `TEMPORAL_NAMESPACE`, `TEMPORAL_ADDR` env vars
- **Helm:** `temporal-ui-deployment.yaml` uses temporal-mcp image (not stock `temporalio/ui`)
- **Build:** `make build-temporal-mcp` target in Makefile

### Test Plan

- Unit tests per package
- Integration test with Temporal Server
