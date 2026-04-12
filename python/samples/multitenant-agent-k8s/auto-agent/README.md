# Auto Agent — Phase 0.9

Autonomous AI assistant with dynamic tool discovery via tool-registry.

## Features

- **ReAct loop** — Think → ListTools → CallTool → repeat
- **Dynamic tools** — discovers available tools at runtime from tool-registry
- **HITL** — pauses before destructive actions and requests approval via kagent executor `input_required` (e.g. kagent UI or a Telegram bot)
- **Memory** — multi-turn via KAgentCheckpointer (PostgreSQL)
- **Org-scoped** — only sees tools registered for its tenant+org

## Env vars

| Var | Default | Description |
|-----|---------|-------------|
| `TENANT_ID` | `unknown` | Tenant identifier |
| `ORG_ID` | `unknown` | Organisation identifier |
| `ORG_NAME` | derived | Human-readable org name |
| `TOOL_REGISTRY_URL` | `http://tool-registry.platform.svc.cluster.local:8080` | Registry URL |
| `REGISTRY_TOKEN` | — | Bearer token (from K8s Secret) |
| `KAGENT_URL` | `http://kagent-controller.kagent.svc.cluster.local:8083` | For call_agent |
| `OPENAI_API_KEY` | — | Required |
| `OPENAI_MODEL` | `gpt-4o-mini` | Model name |
| `OPENAI_BASE_URL` | — | Custom provider (e.g. BotHub) |
| `DATABASE_URL` | — | PostgreSQL for KAgentCheckpointer |
