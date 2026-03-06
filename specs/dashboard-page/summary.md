# Dashboard Page — Summary

## Artifacts

| File | Description |
|------|-------------|
| `specs/dashboard-page/rough-idea.md` | Original idea with ASCII layout sketch |
| `specs/dashboard-page/requirements.md` | 9 Q&A decisions defining scope |
| `specs/dashboard-page/research/ui-structure.md` | Current UI pages, routing, layout |
| `specs/dashboard-page/research/api-data-sources.md` | Available API endpoints and DB models |
| `specs/dashboard-page/research/component-patterns.md` | Shadcn/UI components, styling, theme |
| `specs/dashboard-page/research/streaming-infrastructure.md` | SSE/streaming and live feed status |
| `specs/dashboard-page/design.md` | Detailed design with architecture, components, acceptance criteria |
| `specs/dashboard-page/plan.md` | 13-step implementation plan with checklist |

## Overview

A new Dashboard page at `/` replacing the current AgentList. Shows 7 resource stat cards, a recharts activity chart (mock data, Prometheus/Temporal later), recent runs from DB sessions, and a pseudo-live feed from recent events. Includes a small Go backend endpoint (`GET /api/dashboard/stats`) for aggregated counts and recent data.

## Key Decisions

- **Scope:** Frontend + small backend stats endpoint (not purely frontend-only)
- **Stats:** 7 cards for all resources, static (not clickable)
- **Chart:** recharts with mock data, real data from Prometheus/Temporal in future
- **Data:** New `GET /api/dashboard/stats` endpoint with DB COUNT queries
- **Live Feed:** Pseudo-feed from recent session events (not true streaming)
- **Top Bar:** Namespace selector + "Stream Connected" badge + logout
- **Refresh:** On page load only, no polling

## Suggested Next Steps

1. Review and approve the design and plan
2. Implement via the 13-step plan (backend steps 1-4, frontend steps 5-12, integration step 13)
3. Future: wire activity chart to Prometheus/Temporal, add real SSE live feed, add auto-refresh
