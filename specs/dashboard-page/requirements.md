# Requirements

## Questions & Answers

### Q1: Scope — frontend-only or full-stack?

The sketch shows stats (agent count, run counts, failure rates) and an activity chart that require data the backend doesn't currently aggregate. Should this design:

- **(A) Frontend-only (Phase 1):** Dashboard fetches existing list endpoints (`/api/agents`, `/api/sessions`, `/api/tools`, etc.), counts client-side. Activity chart and run stats are stubbed or omitted until backend support exists.
- **(B) Full-stack:** Includes a new `GET /api/dashboard/stats` backend endpoint that returns pre-aggregated counts, run history buckets, failure rates, and avg duration.
- **(C) Both, phased:** Frontend-first with client-side counts, then a follow-up step adds the backend stats endpoint for the activity chart.

**A1:** (A) Frontend-only. Client-side aggregation from existing list endpoints. Activity chart and run stats stubbed/omitted for now.

### Q2: Stats row — which cards to show?

The sketch shows 7 metric cards: My Agents, Workflows, Cron Jobs, Models, Tools, MCP Servers, GIT Repos. Given that some of these are placeholder pages (Workflows is "Coming soon"), should we:

- **(A) Show all 7** — display counts for all resources, even if some are always 0
- **(B) Show only active resources** — only cards for resources with working pages (Agents, Cron Jobs, Models, Tools, MCP Servers, Git Repos — skip Workflows)
- **(C) Show all 7 but mark placeholders** — show all cards, dim or badge the ones that are "Coming soon"

**A2:** (A) Show all 7 cards. Display counts for all resources regardless of page status.

### Q3: Agent Activity chart — include or stub?

The sketch shows a combined line+bar chart with time-series data (run duration, agent installs, failed buckets). Since we're going frontend-only and there's no time-series backend endpoint, and no chart library is installed:

- **(A) Omit entirely** — skip the chart section for now, just show stats row + bottom panels
- **(B) Stub with placeholder** — show the chart area with "Coming soon" or sample/mock data
- **(C) Install recharts and build with mock data** — wire up the chart UI with realistic-looking static data, ready to connect to a real endpoint later

**A3:** Data for the activity chart will come from Prometheus (workflow metrics from Temporal). So the chart is real but the data source is external (Prometheus/Temporal), not the kagent DB.

**Follow-up:** Should we install recharts now and build the chart UI with mock/placeholder data (ready to wire to Prometheus later), or stub the chart area as "Coming soon"?

**A3 (final):** (B) Install recharts, build chart UI with mock data. Chart will be wired to Prometheus/Temporal later.

### Q4: Recent Runs panel — data source?

The sketch shows a "Recent Runs" list (left bottom panel). The existing `GET /api/sessions` endpoint returns sessions per user. Should "Recent Runs" map to:

- **(A) Recent sessions** — list of most recent agent chat sessions (what exists today)
- **(B) Recent cron job executions** — from cronjob `lastRunTime`/`lastRunResult`
- **(C) Both combined** — merge sessions + cron job runs into a unified "recent activity" list

**A4:** Use the kagent database via a new lightweight backend stats endpoint (`GET /api/dashboard/stats`). This endpoint will run COUNT queries and return recent sessions, avoiding the need to fetch full lists client-side. Scope update: this is no longer purely frontend-only — includes a small backend addition.

### Q5: Live Feed panel — include or stub?

The sketch shows a "Live Feed" mini-panel (right bottom) with a green dot and "0 events". The `/feed` page is currently a placeholder and there's no system-wide event stream backend. Should we:

- **(A) Stub with placeholder** — show the panel frame with "Coming soon" or "No events"
- **(B) Omit entirely** — skip the Live Feed panel for now, make Recent Runs take full width
- **(C) Show recent events from sessions** — pull latest session events as a pseudo-feed (not truly live, but shows recent activity)

**A5:** (C) Show recent session events as a pseudo-feed. Pull latest events from the DB via the stats endpoint. Not truly live/streaming yet, but shows recent activity.

### Q6: Top bar — "Stream Connected" badge and namespace selector?

The sketch shows a top bar with namespace selector dropdown, "Stream Connected" status badge, and logout button. The sidebar already has a namespace selector and StatusIndicator. Should the top bar:

- **(A) Match the sketch exactly** — duplicate namespace selector + add stream status badge + logout in the top bar
- **(B) Simplified** — just show page title ("Dashboard" / subtitle) + a connection status dot in the top bar, since namespace selector is already in the sidebar
- **(C) No top bar** — rely on sidebar for all controls, main content starts with the stats row

**A6:** (A) Match the sketch. Top bar includes namespace selector dropdown, "Stream Connected" status badge (green dot + wifi icon), and logout/exit button. This duplicates the sidebar namespace selector intentionally for quick access in the main content area.

### Q7: Navigation change — what happens to the current AgentList at `/`?

Currently `/` renders AgentList (agent grid). Dashboard will replace it. Where should the agent list move?

- **(A) `/agents` already exists** — the agents page at `/agents` already renders AgentList, so just replace `/` with Dashboard. No move needed.
- **(B) Keep agent grid as a section within the dashboard** — embed a compact agent overview in the dashboard itself

**A7:** (A) Just replace `/` with Dashboard. `/agents` already has the agent list — no move needed.

### Q8: Stat cards — should they be clickable links to their respective pages?

For example, clicking "My Agents (3)" navigates to `/agents`, clicking "Models (4)" goes to `/models`, etc.

- **(A) Yes, clickable** — each card links to its resource page
- **(B) No, static display only**

**A8:** (B) Static display only. Stat cards show counts but are not clickable links.

### Q9: Auto-refresh / polling?

Should the dashboard data refresh automatically, or only on page load?

- **(A) On page load only** — data fetched once when navigating to dashboard
- **(B) Periodic polling** — refresh stats every N seconds (e.g., 30s)
- **(C) Manual refresh** — show a refresh button the user can click

**A9:** (A) On page load only. No auto-refresh or polling.

