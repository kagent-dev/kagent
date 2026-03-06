# Rough Idea

Dashboard page - reference screenshot: `image.png`

## Layout Sketch

```
┌─────────────────────────────────────────────────────────────────────────────┐
│ [browser tabs / top chrome]                                                 │
├──────────────────┬──────────────────────────────────────────────────────────┤
│  [K] KAgent  🌙  │  Namespace: [default ▼]          ● Stream Connected  [→] │
│  [default ▼]     │                                                          │
│                  │  Dashboard                                               │
│  OVERVIEW        │  Overview of your KAgent cluster                         │
│  ⊞ Dashboard     │                                                          │
│  ∿ Live Feed     │  ┌──────────────────────────────────────────────────────┐│
│  🧩 Plugins      │  │ 🤖 MY AGENTS  ⑂ WORKFLOWS  ⏱ CRON JOBS  🧠 MODELS   ││
│                  │  │      3             0              3           4      ││
│  AGENTS          │  │                                                      ││
│  🤖 My Agents    │  │ 🔧 TOOLS   🖥 MCP SERVERS   ⑂ GIT REPOS             ││
│  ⑂ Workflows     │  │      3           2                0                  ││
│  ⏱ Cron Jobs     │  └──────────────────────────────────────────────────────┘│
│                  │                                                          │
│  RESOURCES       │  ┌──────────────────────────────────────────────────────┐│
│  🧠 Models       │  │ Agent Activity                [Avg] P95  1h [24hr] 7d││
│  🔧 Tools        │  │ Runs over time with failed runs highlighted          ││
│  🖥 MCP Servers  │  │                                                      ││
│  ⑂ GIT Repos     │  │ Total runs: 47  Avg duration: 51.0s                  ││
│                  │  │ Failed runs: 39  Failure rate: 83.0%                 ││
│  ADMIN           │  │                                                      ││
│  🏢 Organization │  │  ^ (line chart + bar chart combined)                 ││
│  🌐 Gateways     │  │  |        /\                                         ││
│                  │  │  |       /  \        /\   ■■  /\                     ││
│                  │  │  |______/____\______/  \_/  \/  \_________           ││
│                  │  │  9p  12a  3a  6a  9a  12p  3p  6p  9p                ││
│                  │  │                                                      ││
│  [status footer] │  │ ● Avg run duration  ● Agents installed (bars)        ││
│                  │  │ ● Failed buckets                                     ││
│                  │  └──────────────────────────────────────────────────────┘│
│                  │                                                          │
│                  │  ┌─────────────────────────┐  ┌─────────────────────┐    │
│                  │  │ Recent Runs  View all → │  │ ∿ Live Feed ●       │    │
│                  │  │                         │  │              0 events│   │
│                  │  │  (list of recent runs)  │  │  (live event feed)  │    │
│                  │  └─────────────────────────┘  └─────────────────────┘    │
└──────────────────┴──────────────────────────────────────────────────────────┘
```

## Key UI Elements

### Sidebar (left, dark)
- Header: KAgent logo + "KAgent" label + theme toggle (🌙/☀)
- Namespace selector dropdown (e.g. "default") — inside sidebar header
- Nav sections and items (from `AppSidebarNav`):
  - **OVERVIEW**: Dashboard, Live Feed, Plugins
  - **AGENTS**: My Agents, Workflows, Cron Jobs
  - **RESOURCES**: Models, Tools, MCP Servers, GIT Repos
  - **ADMIN**: Organization, Gateways
  - *(dynamic)* **PLUGINS**: any plugin-registered nav items appended here
- Footer: `StatusIndicator` component (connection/stream status)
- Collapsible to icon-only mode

### Top Bar (main content area)
- Page title: "Dashboard" / subtitle: "Overview of your KAgent cluster"
- Connection status badge: "Stream Connected" (green dot + wifi icon) — top right
- Logout/exit button (top right)

### Stats Row (summary cards)
Six metric cards in a horizontal row (mapped to KAgent resources):
1. My Agents — 3
2. Workflows — 0
3. Cron Jobs — 3
4. Models — 4
5. Tools — 3
6. MCP Servers — 2

### Agent Activity Chart
- Title: "Agent Activity" with subtitle "Runs over time with failed runs highlighted"
- Time range toggle: Avg | P95 | 1h | **24hr** (active) | 7 days
- Summary stats: Total runs, Avg duration (cyan), Failed runs (red), Failure rate
- Combined chart: line (avg run duration) + bar (agents installed) + failed buckets highlighted in teal/green
- X-axis: time labels (9p, 12a, 3a, 6a, 9a, 12p, 3p, 6p, 9p)
- Legend: Avg run duration (blue line), Agents installed (bars, teal), Failed buckets (red dots)

### Bottom Row (two panels)
- **Recent Runs** (left half): list of recent agent runs with "View all →" link
- **Live Feed** (right half): live event feed (replaces "Event Stream"), green dot indicator, shows "0 events" — maps to `/feed` route
