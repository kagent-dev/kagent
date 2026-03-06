# UI Test Plan Prompt

## Goal

Validate that the plugin UI flow is fully functional end-to-end in kagent:

1. Plugin navigation item is visible in the left sidebar.
2. `GET /api/plugins` succeeds and returns expected plugin metadata.
3. Plugin iframe path `/_p/{pathPrefix}/` is reachable.
4. Browser page `/plugins/{pathPrefix}` renders within the kagent shell (sidebar visible).
5. No `502` errors are produced by nginx for plugin flows.

This plan is focused on the known Kanban plugin path (`kanban`) but should be reusable for other plugins.

---

## Scope

### In scope
- Browser validation at `http://localhost:8082`
- Curl validation for UI/API/proxy paths
- Config consistency checks across UI nginx, Next.js, and Go routes
- Runtime wiring checks using kagent-tools (Kubernetes services, endpoints, and connectivity)
- Log validation for regression evidence

### Out of scope
- Refactoring architecture
- Feature development unrelated to plugin routing
- Broad cluster diagnosis unrelated to plugin menu/proxy issues

---

## Preconditions

1. kagent is deployed and UI is reachable at `http://localhost:8082`.
2. The Kanban RemoteMCPServer exists with UI metadata:
   - `ui.enabled: true`
   - `ui.pathPrefix: kanban`
3. A plugin service is deployed and expected to be reachable by the Go backend.
4. Avoid direct `kubectl` in this runbook; use **kagent-tools MCP** for Kubernetes inspection.

---

## Canonical path contract (must hold true)

- **Browser URL:** `/plugins/{pathPrefix}` -> Next.js page with sidebar + iframe shell
- **Proxy URL:** `/_p/{pathPrefix}/` -> Go backend reverse proxy to plugin upstream
- **Discovery API:** `/api/plugins` -> plugin list used by sidebar/nav rendering

If implementation diverges from this contract, record as a blocking inconsistency.

---

## Test matrix

| Area | Check | Pass condition |
|------|-------|----------------|
| UI shell | `/plugins` loads | HTTP 200 and page renders |
| Sidebar status | plugin status indicator | Not "Plugins failed" |
| Sidebar nav | plugin item visible | "Kanban Board" or configured display name appears |
| Plugin page | `/plugins/kanban` | Loads with sidebar retained |
| Plugin proxy | `/_p/kanban/` | Not 502 |
| Plugin API | `/api/plugins` | HTTP 200 + valid JSON payload |
| Backend API | `/api/agents` (sanity) | HTTP 200 (proves API proxy path health) |
| Logs | nginx/upstream errors | No `connect() failed ... 127.0.0.1:8083` during test window |

---

## Step-by-step execution

## Phase 1: Quick HTTP baseline (curl)

Run and capture all outputs:

```bash
curl -si http://localhost:8082/health
curl -si http://localhost:8082/plugins
curl -si http://localhost:8082/api/plugins
curl -si http://localhost:8082/_p/kanban/
curl -si http://localhost:8082/plugins/kanban
curl -si http://localhost:8082/api/agents
```

Expected:
- `/health` -> `200`
- `/plugins` -> `200`
- `/plugins/kanban` -> `200`
- `/api/plugins` -> `200` (JSON)
- `/_p/kanban/` -> non-`502` (200/30x/401 acceptable based on plugin behavior)
- `/api/agents` -> `200`

Record any non-2xx/3xx and include headers/body snippets in findings.

---

## Phase 2: Browser validation (manual + network evidence)

1. Open `http://localhost:8082/plugins`.
2. Confirm sidebar/footer plugin status is healthy (not failed).
3. Confirm plugin nav item appears in expected section.
4. Click plugin nav item; ensure URL becomes `/plugins/kanban`.
5. Confirm page keeps kagent shell/sidebar and iframe area loads plugin content.
6. Hard refresh `http://localhost:8082/plugins/kanban` and verify same behavior.

Capture:
- Screenshot of sidebar with plugin item visible
- Screenshot of loaded plugin page
- Network statuses for:
  - `/api/plugins`
  - `/_p/kanban/`

Fail signatures:
- `Plugins failed` in footer
- missing plugin item
- iframe showing raw nginx `502 Bad Gateway`

---

## Phase 3: Static config consistency audit

Check each file for alignment with canonical path contract:

1. `ui/conf/nginx.conf`
   - `/` -> Next.js UI upstream
   - `/api/` -> Go backend
   - `/_p/` -> Go backend
   - Ensure Go backend target is a reachable service address for cluster deployment
2. `ui/src/app/plugins/[name]/[[...path]]/page.tsx`
   - iframe src uses `/_p/${name}/...`
3. `ui/src/lib/sidebar-status-context.tsx`
   - plugin list fetched from `/api/plugins`
4. `go/core/internal/httpserver/server.go`
   - `GET /api/plugins` route exists
   - reverse proxy route prefix for `/_p/{name}` exists
5. `go/core/internal/httpserver/handlers/pluginproxy.go`
   - strips `/_p/{name}` prefix and forwards remaining path upstream

Mark each file as PASS/FAIL with one-line reason.

---

## Phase 4: Kubernetes runtime wiring checks (kagent-tools MCP only)

Use kagent-tools to verify:

1. Controller service exists and exposes expected port (default `8083`).
2. UI service exists and exposes expected port (default `8080` internal, externally port-forwarded to `8082` local).
3. Endpoint objects for controller and UI services have ready addresses.
4. Kanban service exists with expected port.
5. In-cluster connectivity checks:
   - UI namespace context -> controller service:8083
   - Controller -> kanban service target port

If any service name/port mismatch is found, classify as **wiring inconsistency** and provide exact expected vs actual.

---

## Phase 5: Log validation

Inspect logs during/after reproducing `/plugins` and `/plugins/kanban`.

Look for:
- `connect() failed (111: Connection refused)` while connecting to upstream
- requests to `/api/plugins` returning 502
- requests to `/_p/kanban/` returning 502

Expected in healthy state:
- `/api/plugins` served successfully
- `/_p/kanban/` proxied successfully
- no localhost backend refusal errors in UI nginx logs

---

## Failure classification guide

### Class A: API proxy broken
- Symptom: `/api/plugins` returns `502`
- Likely cause: nginx backend target unreachable or wrong service/port
- User-visible effect: sidebar status failed + plugin menu missing

### Class B: Plugin proxy broken
- Symptom: `/api/plugins` is `200`, but `/_p/kanban/` returns `502/404`
- Likely cause: Go reverse proxy lookup/upstream/plugin service issue
- User-visible effect: nav visible, plugin page broken

### Class C: UI rendering mismatch
- Symptom: APIs are healthy but plugin still not in nav
- Likely cause: sidebar data parsing/filter/section mapping bug

### Class D: Spec/code drift
- Symptom: mixed usage of `/plugins/{name}` as proxy path instead of browser shell path
- Effect: direct URL refresh inconsistencies and debugging confusion

---

## Required output report format

Produce report sections in this order:

1. **Executive summary** (2-5 bullets)
2. **Environment details** (URL, namespace, test timestamp)
3. **Step results table** (each phase pass/fail)
4. **Findings by severity**
   - Critical
   - Major
   - Minor
5. **Evidence**
   - curl snippets
   - browser observations
   - log excerpts
   - kagent-tools checks
6. **Root cause hypothesis per finding**
7. **Suggested fix path**
   - immediate hotfix
   - durable fix
   - regression tests to add
8. **Exit criteria status**

---

## Exit criteria (definition of done)

All must be true:

1. `GET /api/plugins` returns `200` from UI entrypoint.
2. `GET /_p/kanban/` no longer returns `502`.
3. Sidebar shows plugin nav item.
4. `/plugins/kanban` loads plugin content in iframe while preserving sidebar.
5. Hard refresh on `/plugins/kanban` preserves full shell and plugin behavior.
6. No upstream connection-refused errors for controller proxy path in test logs.

If any criterion fails, test outcome is **FAILED** and must include remediation plan.

---

## Build-and-execute plan for Kanban UI fix

Use this when the request is "verify `/plugins/kanban` and make Kanban UI work".

### Step 1: Verify runtime quickly

Run:

```bash
curl -si http://localhost:8082/health
curl -si http://localhost:8082/api/plugins
curl -si http://localhost:8082/_p/kanban/
curl -si http://localhost:8082/plugins/kanban
```

Pass gate:
- All routes return `200` or acceptable redirect/auth (no `502`).
- `/api/plugins` includes `pathPrefix: "kanban"`.

### Step 2: Verify shell + iframe contract

Confirm:
- Browser route remains `/plugins/kanban`.
- Plugin iframe source points to `/_p/kanban/...`.
- Sidebar stays visible and plugin item is present.

### Step 3: Fix regressions discovered during verification

Prioritize in this order:
1. Routing/proxy path mismatches (`/plugins/*` vs `/_p/*`).
2. Sidebar plugin loading/status regressions.
3. Test expectation drift after nav or plugin UI changes.

### Step 4: Re-run focused tests

```bash
cd ui
npm test -- --runTestsByPath \
  src/components/sidebars/__tests__/AppSidebar.test.tsx \
  src/components/sidebars/__tests__/AppSidebarNav.test.tsx \
  src/components/sidebars/__tests__/StatusIndicator.test.tsx
```

### Step 5: Record outcomes

Document:
- Which endpoint failed/passed.
- Exact code files changed.
- Which tests failed before fix and passed after fix.

---

## Execution record (2026-03-06)

### Runtime verification
- `GET /health`: `200`
- `GET /api/plugins`: `200` with Kanban plugin metadata (`pathPrefix: "kanban"`)
- `GET /_p/kanban/`: `200`
- `GET /plugins/kanban`: `200`
- `GET /api/agents`: `200`

### Fixes applied
- Updated sidebar nav unit test expectations for the new static `Plugins` nav item:
  - `ui/src/components/sidebars/__tests__/AppSidebarNav.test.tsx`
  - static item count changed from 11 to 12
  - expected label list now includes `Plugins`
  - non-active count adjusted accordingly

### Validation status
- Focused sidebar tests now pass after updating expectations.
- Kanban UI route and proxy endpoint are healthy from UI entrypoint (`localhost:8082`).

---

## Optional automation follow-up

After manual pass, run/extend automated checks:

- Cypress plugin routing tests under `ui/cypress/e2e/plugin-routing.cy.ts`
- API smoke script for plugin endpoints
- Post-deploy smoke in CI:
  - `/api/plugins`
  - `/_p/kanban/`
  - browser screenshot assertion for sidebar plugin visibility

---

## Spec Audit Findings (2026-03-06)

Full review of all specs in `specs/` against actual implementations.

### Summary matrix

| Spec | Status | Critical gaps |
|------|--------|---------------|
| **dynamic-mcp-ui-routing** | Mostly done | Auth token forwarding missing; no CI E2E workflow |
| **pluggable-ui-k8s-plugins** | Done | None |
| **mcp-kanban-server** | Done | None |
| **temporal-workflows-ui** | Not started | Entire plugin not built |
| **temporal-agent-workflow** | Not started | Full backend implementation missing |
| **git-repos-api-ui** | Partial | Git MCP server not built; `/git` page is stub |
| **ai-cron-jobs** | Partial | Controller scheduling missing; `/cronjobs` UI is stub |
| **ui-test-pan** (this doc) | Active | Playwright gap; CI integration gap |

---

### Gap 1 — `dynamic-mcp-ui-routing`: Auth token not forwarded to plugin iframe

**Spec reference:** `specs/dynamic-mcp-ui-routing/design.md` §7 (Auth), plan Step 11.

**Current state:** `authToken: null` placeholder in the postMessage `kagent:context` payload sent to the plugin iframe.

**User-visible effect:** Plugins requiring auth will fail silently inside the iframe; no error is surfaced.

**Files to fix:**
- `ui/src/app/plugins/[name]/[[...path]]/page.tsx` — populate `authToken` from the active session/cookie before sending `kagent:context`.

**Steps to fix:**
1. Read the current session token from the auth context or cookie in the plugin page component.
2. Pass the token in the `postMessage` payload: `authToken: token`.
3. Add a unit test in `ui/src/app/plugins/__tests__/` asserting the message includes a non-null `authToken` when a session is active.

---

### Gap 2 — `dynamic-mcp-ui-routing`: No CI workflow for E2E or API smoke

**Spec reference:** `specs/dynamic-mcp-ui-routing/plan.md` Step 16 — "Create GitHub Actions workflow for E2E tests".

**Current state:** `.github/workflows/` has no `e2e-browser.yml`. Cypress tests exist locally but are not run in CI. The `scripts/check-plugins-api.sh` script exists but is not invoked from CI.

**User-visible effect:** Plugin routing regressions are not caught automatically on PRs.

**Steps to fix:**
1. Create `.github/workflows/e2e-browser.yml` that:
   - Builds the stack with `make helm-install` on a kind cluster.
   - Runs `scripts/check-plugins-api.sh`.
   - Runs `npx cypress run --spec cypress/e2e/plugin-routing.cy.ts`.
2. Add the workflow as a required check on the `main` branch protection rule.

---

### Gap 3 — `dynamic-mcp-ui-routing`: Plugin bridge JS is inline, not standalone

**Spec reference:** `specs/dynamic-mcp-ui-routing/design.md` §9 — "kagent-plugin-bridge.js as reusable standalone file".

**Current state:** Bridge logic (postMessage handling for `kagent:context`, `kagent:ready`, `kagent:badge`) is embedded inline inside `go/plugins/kanban-mcp/internal/ui/index.html`. Future plugin authors must copy-paste it.

**Steps to fix:**
1. Extract the bridge script to `go/plugins/kanban-mcp/internal/ui/kagent-plugin-bridge.js` (served as a static asset from the kanban MCP server, or hosted from the Go backend).
2. Replace inline code in `index.html` with `<script src="...kagent-plugin-bridge.js"></script>`.
3. Document usage in `specs/dynamic-mcp-ui-routing/design.md` so future plugin authors know to include it.

---

### Gap 4 — `temporal-workflows-ui`: Custom plugin not built

**Spec reference:** `specs/temporal-workflows-ui/requirements.md` — entire custom temporal-workflows MCP plugin.

**Current state:** No implementation exists. The Workflows nav item at `/workflows` is a stub.

**Scope of work:**
- New MCP server: `temporal-workflows-mcp` with tools `list_workflows`, `get_workflow`, `cancel_workflow`, `signal_workflow`.
- Embedded SPA (similar to Kanban) showing workflow list, detail view, status filters.
- Helm chart under `helm/tools/temporal-workflows-mcp/` with a `RemoteMCPServer` YAML that sets `ui.enabled: true`.
- kagent bridge integration (reuse bridge JS from Gap 3).

**Steps to fix:**
1. Create `go/plugins/temporal-workflows-mcp/` following the kanban-mcp structure.
2. Implement the four MCP tools connecting to the Temporal gRPC API.
3. Build the embedded SPA with Temporal workflow list/detail/status pages.
4. Add `helm/tools/temporal-workflows-mcp/` Helm chart.
5. Update `specs/temporal-workflows-ui/requirements.md` as each milestone is completed.

---

### Gap 5 — `git-repos-api-ui`: Git MCP server and full UI not implemented

**Spec reference:** `specs/git-repos-api-ui/plan.md` — git repo MCP server + full `/git` UI.

**Current state:**
- Go HTTP handlers in `go/core/internal/httpserver/handlers/gitrepos.go` proxy to a `gitrepo-mcp` service that does not exist.
- The `/git` page in the UI is a stub with no list, add, remove, or search functionality.

**Steps to fix:**
1. Implement `gitrepo-mcp` service (or confirm it lives in another repo and wire it up).
2. Build `/git` Next.js page:
   - List git repos from `/api/gitrepos`
   - Add / remove repo forms
   - Search + index trigger UI
3. Add Cypress tests for the `/git` page to `ui/cypress/e2e/smoke.cy.ts`.
4. Add Helm chart or sub-chart for `gitrepo-mcp` under `helm/tools/`.

---

### Gap 6 — `ai-cron-jobs`: Controller scheduling logic and `/cronjobs` UI missing

**Spec reference:** `specs/ai-cron-jobs/plan.md` — scheduler controller + session creation + status UI.

**Current state:**
- `AgentCronJob` CRD is defined.
- CRUD HTTP handlers exist (`go/core/internal/httpserver/handlers/cronjobs.go`).
- The controller does not execute schedules (no timer/cron loop and no HTTP session creation calls).
- The `/cronjobs` page in the UI is a stub.

**Steps to fix:**
1. In the `AgentCronJobReconciler`, implement a cron scheduling loop using `robfig/cron` or `time.AfterFunc`.
2. On each trigger: create a session via the kagent HTTP API with the configured prompt and agent ref.
3. Update `AgentCronJob.Status` with `lastRunTime`, `lastRunStatus`, `nextRunTime`, `lastSessionID`.
4. Build the `/cronjobs` Next.js page: list jobs, per-job status badge, run history table, enable/disable toggle.
5. Add unit tests for the controller scheduling logic and handler CRUD.

---

### Gap 7 — `ui-test-pan` (this doc): Cypress not integrated in CI

**Current state:** All Cypress tests live under `ui/cypress/e2e/` and pass locally. There is no CI job that executes them.

**Steps to fix:**
1. Add `cypress run` step to the existing `ui-tests` CI job or create a new `cypress-e2e` job.
2. Use `cypress-io/github-action` for dependency caching.
3. Fail the PR check if any Cypress spec fails.

---

### Prioritized fix roadmap

| Priority | Gap | Effort | Owner area |
|----------|-----|--------|------------|
| P0 | Gap 2 — CI E2E workflow | Small | DevOps / CI |
| P0 | Gap 7 — Cypress in CI | Small | DevOps / CI |
| P1 | Gap 1 — Auth token forwarding | Small | UI |
| P1 | Gap 3 — Standalone bridge JS | Small | Go plugins |
| P2 | Gap 6 — ai-cron-jobs controller + UI | Medium | Go + UI |
| P2 | Gap 5 — git-repos-api-ui MCP + UI | Medium | Go + UI |
| P3 | Gap 4 — temporal-workflows-ui plugin | Large | Go plugins + UI |

