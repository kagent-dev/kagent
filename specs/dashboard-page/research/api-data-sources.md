# API & Data Sources Research

## Available Endpoints for Dashboard Data

### Counts (client-side aggregation from list endpoints)
| Resource | Endpoint | Notes |
|----------|----------|-------|
| Agents | `GET /api/agents` | deploymentReady, accepted status |
| Sessions/Runs | `GET /api/sessions` | per-user, with timestamps |
| Tasks | `GET /api/sessions/{id}/tasks` | per-session |
| Tools | `GET /api/tools` | all MCP tools |
| Tool Servers | `GET /api/toolservers` | lastConnected timestamp |
| Models | `GET /api/modelconfigs` | LLM configurations |
| Cron Jobs | `GET /api/cronjobs` | schedule, lastRunTime, nextRunTime |
| Git Repos | `GET /api/gitrepos` | sync status |
| Feedback | `GET /api/feedback` | isPositive, issueType |

### Missing (would need new backend endpoints)
- **No `/api/dashboard/stats`** — no aggregation endpoint
- **No time-series aggregation** — no hourly/daily bucketing
- **No run duration tracking** — Task model has no duration field
- **No success/failure status on tasks** — no explicit status enum
- **No token usage tracking** — not in DB models

## Database Models (relevant)

```
Session { id, name, user_id, agent_id, created_at, updated_at }
  -> Events { id, session_id, user_id, data(JSON), created_at }
  -> Tasks { id, session_id, data(JSON), created_at }

Agent { id, type, config(JSON), created_at }
Tool { id, server_name, group_kind, description }
ToolServer { name, group_kind, last_connected }
Feedback { id, user_id, message_id, is_positive, feedback_text, issue_type }
```

## Server Actions (UI fetch functions)
All in `ui/src/app/actions/`:
- `agents.ts` — getAgents(), getAgent()
- `sessions.ts` — getSessionsForAgent(), getSession()
- `tools.ts` — getTools()
- `servers.ts` — tool servers
- `modelConfigs.ts` — model configs
- `models.ts` — LLM models
- `cronjobs.ts` — cron jobs
- `gitrepos.ts` — git repos
- `plugins.ts` — plugins
- `namespaces.ts` — K8s namespaces

## Strategy for Dashboard Stats

### Phase 1: Client-side aggregation
- Fetch all list endpoints in parallel
- Count items client-side
- Use session timestamps for "recent runs" list
- Cron job `lastRunTime`/`lastRunResult` for activity

### Phase 2: Backend stats endpoint
- New `GET /api/dashboard/stats` returning counts + time-series
- Add run duration/status tracking to Task model
- Add hourly bucketed activity data
