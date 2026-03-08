# EP-2009: Dashboard Overview Page

* Status: **Implemented**
* Spec: [specs/dashboard-page](../specs/dashboard-page/)

## Background

New Dashboard page at `/` replacing the AgentList as the application homepage. Shows aggregated resource counts, recent activity, and system status at a glance.

## Motivation

Users need a quick overview of their kagent deployment — how many agents, tools, workflows exist, and what recent activity has occurred — without navigating to individual pages.

### Goals

- 7 resource stat cards: Agents, Workflows, CronJobs, Models, Tools, MCPServers, GitRepos
- Recent runs panel from DB sessions
- Recent events feed from session events
- Activity chart placeholder (mock data, Prometheus/Temporal integration later)
- Backend stats endpoint: `GET /api/dashboard/stats`

### Non-Goals

- Real-time streaming dashboard (pseudo-feed from recent events only)
- Clickable stat cards with drill-down
- Prometheus metrics integration (future)

## Implementation Details

- **Backend:** `go/core/internal/httpserver/handlers/dashboard.go` — aggregated DB COUNT queries
- **API types:** `DashboardStatsResponse` in `go/api/httpapi/types.go`
- **UI components:** `StatCard`, `StatsRow`, `RecentRunsPanel`, `LiveFeedPanel`, `ActivityChart`, `DashboardTopBar`
- **Page:** `ui/src/app/page.tsx`

### Test Plan

- Backend unit tests for stats aggregation
- UI component rendering tests
