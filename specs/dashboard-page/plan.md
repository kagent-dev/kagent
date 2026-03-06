# Dashboard Page — Implementation Plan

## Checklist

- [ ] Step 1: Backend — Dashboard stats response types
- [ ] Step 2: Backend — DB client methods for counts and recents
- [ ] Step 3: Backend — Dashboard HTTP handler + route registration
- [ ] Step 4: Backend — Handler unit tests
- [ ] Step 5: Frontend — TypeScript types + server action
- [ ] Step 6: Frontend — StatCard + StatsRow components
- [ ] Step 7: Frontend — Install recharts + ActivityChart component
- [ ] Step 8: Frontend — RecentRunsPanel component
- [ ] Step 9: Frontend — LiveFeedPanel component
- [ ] Step 10: Frontend — DashboardTopBar component
- [ ] Step 11: Frontend — Dashboard page (wire everything together)
- [ ] Step 12: Frontend — Component tests
- [ ] Step 13: Integration — End-to-end verification

---

## Step 1: Backend — Dashboard stats response types

**Objective:** Define the API response types for the dashboard stats endpoint.

**Implementation:**
- Edit `go/api/httpapi/types.go` — add `DashboardStatsResponse`, `DashboardCounts`, `RecentRun`, `RecentEvent` structs
- All fields have JSON tags matching the frontend contract

**Test requirements:**
- Types compile correctly (verified by build)

**Integration notes:**
- These types are shared between handler and frontend — define them first to establish the contract

**Demo:** `go build ./...` passes with new types

---

## Step 2: Backend — DB client methods for counts and recents

**Objective:** Add database query methods needed by the dashboard handler.

**Implementation:**
- Edit `go/api/database/client.go` — add interface methods:
  - `CountSessions(userID string) (int64, error)`
  - `RecentSessions(userID string, limit int) ([]Session, error)`
  - `RecentEvents(limit int) ([]Event, error)`
- Edit DB implementation (e.g., `go/core/internal/database/`) — implement with GORM:
  - `CountSessions`: `db.Model(&Session{}).Where("user_id = ?", userID).Count(&count)`
  - `RecentSessions`: `db.Where("user_id = ?", userID).Order("updated_at DESC").Limit(limit).Find(&sessions)`
  - `RecentEvents`: `db.Order("created_at DESC").Limit(limit).Find(&events)`

**Test requirements:**
- Unit tests for each DB method with mock/test DB

**Integration notes:**
- These methods are consumed by the handler in Step 3

**Demo:** DB methods return correct counts and ordered results against test data

---

## Step 3: Backend — Dashboard HTTP handler + route registration

**Objective:** Create the handler that serves `GET /api/dashboard/stats` and register the route.

**Implementation:**
- Create `go/core/internal/httpserver/handlers/dashboard.go`:
  - `DashboardHandler` struct with DB client + K8s list dependencies
  - `HandleDashboardStats(w, r)` method:
    1. Get userID from auth context
    2. Fetch K8s resource counts (agents, workflows, cronjobs, models, MCP servers) via existing list logic — count results, return 0 on error
    3. Fetch DB counts (tools, tool servers) via DB client
    4. Fetch recent sessions (limit 10) and recent events (limit 20)
    5. Build `DashboardStatsResponse` and write JSON
- Edit `go/core/internal/httpserver/server.go`:
  - Add `Dashboard` field to handlers struct
  - Register route: `GET /api/dashboard/stats` → `HandleDashboardStats`
- Wire handler in server initialization with DB client and K8s dependencies

**Test requirements:**
- Deferred to Step 4

**Integration notes:**
- Handler reuses existing K8s list patterns from other handlers for resource counts
- Graceful degradation: if a K8s resource type fails (CRD not installed), return count 0

**Demo:** `curl localhost:8080/api/dashboard/stats` returns JSON with counts, recent runs, and events

---

## Step 4: Backend — Handler unit tests

**Objective:** Test the dashboard handler with mocked dependencies.

**Implementation:**
- Create `go/core/internal/httpserver/handlers/dashboard_test.go`:
  - Mock DB client returning known counts/sessions/events
  - Mock K8s list responses
  - Table-driven tests:
    - Happy path: all resources available, verify response shape
    - Partial failure: some K8s lists fail, verify 0 counts (no error)
    - Empty state: no sessions or events, verify empty arrays
    - Auth: verify userID is extracted and passed to DB

**Test requirements:**
- All tests pass with `go test ./go/core/internal/httpserver/handlers/ -run TestDashboard`

**Integration notes:**
- Follow existing handler test patterns in the `handlers/` directory

**Demo:** `go test` passes, handler tested with 3+ scenarios

---

## Step 5: Frontend — TypeScript types + server action

**Objective:** Define the frontend types and data fetching function.

**Implementation:**
- Edit `ui/src/types/index.ts` — add `DashboardCounts`, `RecentRun`, `RecentEvent`, `DashboardStatsResponse` interfaces
- Create `ui/src/app/actions/dashboard.ts`:
  - `getDashboardStats()` function using `fetchApi("/api/dashboard/stats")`

**Test requirements:**
- Types compile correctly (verified by `npm run build`)

**Integration notes:**
- Server action follows the same pattern as other actions in `ui/src/app/actions/`

**Demo:** Types available for import, action callable from components

---

## Step 6: Frontend — StatCard + StatsRow components

**Objective:** Build the stat card grid showing 7 resource counts.

**Implementation:**
- Create `ui/src/components/dashboard/StatCard.tsx`:
  - Props: `{ icon: LucideIcon, label: string, count: number }`
  - Uses Shadcn `Card` with centered layout
  - Icon (muted color) + uppercase label (small text) + count (large bold text)
- Create `ui/src/components/dashboard/StatsRow.tsx`:
  - Props: `{ counts: DashboardCounts }`
  - Maps counts to 7 StatCards with correct icons (Bot, GitBranch, Clock, Brain, Wrench, Server, GitFork)
  - Responsive grid: `grid-cols-2 sm:grid-cols-4 lg:grid-cols-7 gap-4`

**Test requirements:**
- StatCard renders icon, label, and count
- StatsRow renders 7 cards with correct data mapping

**Integration notes:**
- Icons match sidebar nav icons from `AppSidebarNav.tsx`

**Demo:** StatsRow renders with sample data, responsive at breakpoints

---

## Step 7: Frontend — Install recharts + ActivityChart component

**Objective:** Add recharts dependency and build the activity chart with mock data.

**Implementation:**
- Install: `npm install recharts` in `ui/`
- Create `ui/src/components/dashboard/ActivityChart.tsx`:
  - Mock data: `MOCK_ACTIVITY_DATA` — 24 hourly data points with `time`, `avgDuration`, `agentRuns`, `failedRuns`
  - Summary stats row: Total runs, Avg duration (cyan), Failed runs (red), Failure rate
  - Time range toggle using Shadcn Tabs (Avg | P95 | 1h | 24hr | 7d) — visual only, doesn't filter
  - recharts `ResponsiveContainer` + `ComposedChart`:
    - `Line` for avg duration (using `--chart-1` color)
    - `Bar` for agent runs (using `--chart-2` color)
    - `Bar` for failed runs (using `--chart-3` / destructive color)
  - `XAxis`, `YAxis`, `Tooltip`, `Legend`
  - Wrapped in Shadcn Card

**Test requirements:**
- Component renders without errors
- Mock data is displayed (chart renders with data points)

**Integration notes:**
- Uses CSS variable chart colors for theme consistency
- Mock data exported separately so it's easy to swap for Prometheus data later
- `ResponsiveContainer` ensures chart resizes with parent

**Demo:** Chart renders with combined line+bar visualization and legend

---

## Step 8: Frontend — RecentRunsPanel component

**Objective:** Show a list of recent agent sessions.

**Implementation:**
- Create `ui/src/components/dashboard/RecentRunsPanel.tsx`:
  - Props: `{ runs: RecentRun[] }`
  - Shadcn Card with header "Recent Runs" + "View all" link to `/agents`
  - List items: agent name (bold) + session name + relative time ("2m ago")
  - Empty state: "No recent runs" with muted text
  - ScrollArea with max height for overflow

**Test requirements:**
- Renders list of runs with correct data
- Shows empty state when runs array is empty
- "View all" link points to `/agents`

**Integration notes:**
- Use `formatDistanceToNow` from date-fns (already in project) or simple relative time helper

**Demo:** Panel shows run list with agent names and timestamps

---

## Step 9: Frontend — LiveFeedPanel component

**Objective:** Show a pseudo-feed of recent session events.

**Implementation:**
- Create `ui/src/components/dashboard/LiveFeedPanel.tsx`:
  - Props: `{ events: RecentEvent[] }`
  - Shadcn Card with header "Live Feed" + green dot indicator + event count badge
  - List items: event summary text + relative timestamp
  - Empty state: "No events" with "0 events" badge
  - ScrollArea with max height for overflow

**Test requirements:**
- Renders list of events with correct data
- Shows event count in header badge
- Shows empty state when events array is empty

**Integration notes:**
- Green dot uses same styling pattern as StatusIndicator (`bg-green-500 rounded-full`)

**Demo:** Panel shows event list with summaries and timestamps

---

## Step 10: Frontend — DashboardTopBar component

**Objective:** Build the top bar with namespace selector, stream status, and logout.

**Implementation:**
- Create `ui/src/components/dashboard/DashboardTopBar.tsx`:
  - Flex row: left side (namespace selector) + right side (status badge + logout)
  - Namespace selector: reuse existing `NamespaceSelector` component or pattern from sidebar
  - Stream status badge: green dot + Wifi icon + "Stream Connected" text (visual-only, always "connected")
  - Logout button: LogOut icon button

**Test requirements:**
- Renders namespace selector, status badge, and logout button
- Status badge shows "Stream Connected" with green indicator

**Integration notes:**
- Uses `useNamespace()` context from existing namespace provider
- Logout behavior: TBD based on existing auth patterns (may just be visual for now)

**Demo:** Top bar renders with all three controls

---

## Step 11: Frontend — Dashboard page (wire everything together)

**Objective:** Replace the current AgentList at `/` with the full Dashboard page.

**Implementation:**
- Edit `ui/src/app/page.tsx`:
  - Remove AgentList import and rendering
  - Create Dashboard client component that:
    1. Calls `getDashboardStats()` on mount
    2. Manages loading/error/data states
    3. Renders: DashboardTopBar → page title/subtitle → StatsRow → ActivityChart → bottom row (RecentRunsPanel + LiveFeedPanel)
  - Loading state: skeleton cards + chart placeholder
  - Error state: reuse existing ErrorState pattern with retry

**Test requirements:**
- Page renders all sections in correct layout order
- Loading state shows skeletons
- Error state shows retry button

**Integration notes:**
- Layout: `space-y-6` vertical stack for sections
- Bottom row: `grid grid-cols-1 md:grid-cols-2 gap-6`
- Page title: "Dashboard" with subtitle "Overview of your KAgent cluster"

**Demo:** Navigate to `/` — full dashboard renders with stats, chart, runs, and feed

---

## Step 12: Frontend — Component tests

**Objective:** Add unit tests for all dashboard components.

**Implementation:**
- Create test files alongside components:
  - `ui/src/components/dashboard/__tests__/StatCard.test.tsx`
  - `ui/src/components/dashboard/__tests__/StatsRow.test.tsx`
  - `ui/src/components/dashboard/__tests__/RecentRunsPanel.test.tsx`
  - `ui/src/components/dashboard/__tests__/LiveFeedPanel.test.tsx`
- Test each component with mock props, verify:
  - Correct rendering of data
  - Empty states
  - Links and navigation

**Test requirements:**
- All component tests pass with `npm test`

**Integration notes:**
- Follow existing test patterns from `ui/src/components/sidebars/__tests__/`

**Demo:** `npm test` passes with all dashboard component tests green

---

## Step 13: Integration — End-to-end verification

**Objective:** Verify the full stack works together.

**Implementation:**
- Build Go backend: `make -C go build`
- Build UI: `make -C ui build`
- Run `make lint` — ensure no lint errors
- Manual verification against the design sketch:
  - 7 stat cards render with counts
  - Activity chart shows mock data with line + bars
  - Recent Runs panel shows sessions
  - Live Feed panel shows events
  - Top bar has namespace selector, status badge, logout
  - Responsive layout works at desktop/tablet/mobile breakpoints

**Test requirements:**
- All Go tests pass: `make -C go test`
- All UI tests pass: `make -C ui test`
- Build succeeds: `make build`
- Lint passes: `make lint`

**Integration notes:**
- Compare rendered page against rough-idea.md layout sketch

**Demo:** Full dashboard page running locally, matching the design sketch
