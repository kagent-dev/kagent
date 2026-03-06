# Live Feed & Streaming Infrastructure Research

## Current State
- `/feed` route: **placeholder** ("Coming soon")
- No dedicated live feed backend endpoint
- No event bus or pub/sub for system-wide events

## Existing SSE Infrastructure (A2A Streaming)

A fully functional SSE pipeline exists for agent chat:

```
Browser (ChatInterface)
  -> POST /a2a/{namespace}/{agentName}
Next.js API Route (proxy + keep-alive)
  -> POST /a2a/{namespace}/{agentName}/
Go Backend (A2A Handler Mux)
  -> Agent Runtime (Python)
  <- SSE events back up pipeline
```

### Key Components
| Layer | File | Role |
|-------|------|------|
| SSE Client | `ui/src/lib/a2aClient.ts` | Parse SSE, async iterable |
| Proxy | `ui/src/app/a2a/[ns]/[name]/route.ts` | Keep-alive (30s), stream forwarding |
| Backend | `go/core/internal/a2a/a2a_handler_mux.go` | Request multiplexing |
| Registrar | `go/core/internal/a2a/a2a_registrar.go` | Dynamic handler registration |
| Middleware | `go/core/internal/httpserver/middleware.go` | HTTP Flusher support |

### Features
- Protocol: SSE (`text/event-stream`)
- Keep-alive: 30s comment events
- Client timeout: 10 minutes
- Cancellation: AbortController
- Flushing: immediate (`FlushInterval: -1`)

## StatusIndicator
**Not streaming.** Simple HTTP fetch of `/api/plugins` with 3 states: loading, ok, plugins-failed. Has retry button.

## Implications for Dashboard

### Live Feed Panel
The dashboard sketch shows a "Live Feed" mini-panel. Options:
1. **Embed session events** — poll `GET /api/sessions` + events periodically
2. **New SSE endpoint** — `GET /api/feed` streaming system events (agent starts, completions, errors)
3. **Reuse A2A infra** — adapt existing SSE patterns for a system-wide event stream

### "Stream Connected" Badge
The top bar shows "Stream Connected" status. This would need:
- A persistent SSE connection for system events
- Connection state tracking (connected/disconnected/reconnecting)
- Could reuse patterns from A2A keep-alive
