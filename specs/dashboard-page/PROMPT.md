# Dashboard Page

## Objective

Add a Dashboard page at `/` replacing the current AgentList home page. The dashboard shows resource counts, an activity chart (mock data), recent runs, and a live event feed. Includes a Go backend stats endpoint and a recharts-based frontend.

## Key Requirements

1. **Backend endpoint** `GET /api/dashboard/stats` — returns resource counts (7 types), recent sessions (limit 10), recent events (limit 20) via DB COUNT queries and K8s list calls
2. **7 stat cards** — Agents, Workflows, Cron Jobs, Models, Tools, MCP Servers, Git Repos (static, not clickable)
3. **Activity chart** — recharts ComposedChart (line + bar) with mock data; real Prometheus/Temporal data later
4. **Recent Runs panel** — list of recent sessions with agent name + relative timestamp
5. **Live Feed panel** — pseudo-feed of recent session events from DB (not truly live)
6. **Top bar** — namespace selector, "Stream Connected" badge (green dot + wifi icon), logout button
7. **Replace `/` route** — remove AgentList from page.tsx (it already exists at `/agents`)
8. **Data on page load only** — no auto-refresh or polling
9. **Graceful degradation** — if a K8s resource type fails (CRD not installed), return count 0

## Acceptance Criteria

- Given a user navigates to `/`, then the Dashboard page renders (not AgentList)
- Given the dashboard loads, then 7 stat cards display correct counts from the stats endpoint
- Given the dashboard loads, then the Activity Chart renders with mock data using recharts
- Given the dashboard loads, then Recent Runs shows up to 10 sessions with agent name and relative time
- Given the dashboard loads, then Live Feed shows up to 20 events with summary and relative time
- Given the stats endpoint is unreachable, then an error state with retry button is shown
- Given a K8s CRD is not installed, then that resource count returns 0 (no error)
- Given a desktop viewport, then stat cards render in a single row of 7
- Given a mobile viewport, then stat cards render in a 2-column grid

## Reference

Full specs at `specs/dashboard-page/`:
- `design.md` — architecture, components, interfaces, data models, error handling
- `plan.md` — 13-step implementation plan with checklist
- `requirements.md` — 9 Q&A decisions defining scope
- `research/` — codebase research on UI structure, API sources, components, streaming

## Implementation Steps

1. Backend: Add response types to `go/api/httpapi/types.go`
2. Backend: Add DB methods (`CountSessions`, `RecentSessions`, `RecentEvents`)
3. Backend: Create handler `go/core/internal/httpserver/handlers/dashboard.go` + register route
4. Backend: Handler unit tests (happy path, partial failure, empty state)
5. Frontend: Add TS types to `ui/src/types/index.ts` + server action `ui/src/app/actions/dashboard.ts`
6. Frontend: `StatCard` + `StatsRow` components in `ui/src/components/dashboard/`
7. Frontend: Install recharts + `ActivityChart` component with mock data
8. Frontend: `RecentRunsPanel` component
9. Frontend: `LiveFeedPanel` component
10. Frontend: `DashboardTopBar` component
11. Frontend: Wire everything in `ui/src/app/page.tsx`
12. Frontend: Component tests
13. Integration: Build, lint, end-to-end verification
