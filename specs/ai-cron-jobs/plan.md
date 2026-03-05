# AgentCronJob — Implementation Plan

## Checklist

- [ ] Step 1: CRD type definition and code generation
- [ ] Step 2: Controller with scheduling logic
- [ ] Step 3: HTTP server CRUD endpoints
- [ ] Step 4: UI list page and server actions
- [ ] Step 5: UI create/edit form
- [ ] Step 6: E2E tests
- [ ] Step 7: Helm chart and RBAC updates

---

## Step 1: CRD Type Definition and Code Generation

**Objective:** Define the `AgentCronJob` CRD types and generate deepcopy/CRD manifests.

**Implementation guidance:**
- Create `go/api/v1alpha2/agentcronjob_types.go` with `AgentCronJob`, `AgentCronJobSpec`, `AgentCronJobStatus`, `AgentCronJobList`
- Add kubebuilder markers: `+kubebuilder:object:root=true`, `+subresource:status`, `+storageversion`, `+printcolumn` for Schedule, Agent, LastRun, NextRun, LastResult
- Register types in `go/api/v1alpha2/groupversion_info.go` via `init()` — add `&AgentCronJob{}` and `&AgentCronJobList{}` to `SchemeBuilder.Register()`
- Run `make -C go generate` to produce `zz_generated.deepcopy.go` entries and CRD YAML

**Test requirements:**
- Verify `make -C go generate` succeeds without errors
- Verify CRD YAML is generated in `config/crd/bases/`
- Apply CRD to a test cluster: `kubectl apply -f config/crd/bases/kagent.dev_agentcronjobs.yaml`
- Create a sample CR and verify it's accepted: `kubectl apply` + `kubectl get agentcronjobs`

**Integration notes:**
- CRD YAML needs to be added to `helm/kagent-crds/` chart templates
- Sample manifest: `examples/agentcronjob.yaml`

**Demo:** `kubectl apply` a sample AgentCronJob, `kubectl get agentcronjobs` shows the resource with print columns.

---

## Step 2: Controller with Scheduling Logic

**Objective:** Implement the controller that watches AgentCronJob CRs, calculates schedules, and triggers agent runs via the HTTP API.

**Implementation guidance:**
- Add `github.com/robfig/cron/v3` dependency: `go get github.com/robfig/cron/v3`
- Create `go/internal/controller/agentcronjob_controller.go`:
  - `AgentCronJobReconciler` struct with `client.Client`, `Scheme`, HTTP base URL, HTTP client
  - RBAC markers for `agentcronjobs`, `agentcronjobs/status`, `agents` (get/list/watch)
  - `SetupWithManager`: watch `AgentCronJob` with `GenerationChangedPredicate`
  - `Reconcile` logic:
    1. Fetch AgentCronJob CR
    2. Parse cron schedule with `cron.ParseStandard(spec.Schedule)` — if invalid, set `Accepted=False` condition, return no requeue
    3. Set `Accepted=True` condition
    4. Calculate next run time from schedule. If `status.lastRunTime` is nil, use CR creation time as reference
    5. If `now >= nextRunTime`: execute (create session, send prompt via HTTP), update status fields
    6. Calculate next run from `now`, set `status.nextRunTime`, return `RequeueAfter(nextRun - now)`
- Execution helper (private method):
  1. `POST /api/sessions` with `agent_ref` = spec.agentRef, `name` = `"cronjob-{name}-{timestamp}"`
  2. `POST /api/a2a/{namespace}/{agentName}` with JSON-RPC message containing spec.prompt and session contextID
  3. Use synchronous `message/send` method (not streaming) for simplicity — controller just needs success/failure
  4. Return session ID and error
- Register controller in `go/pkg/app/app.go` following existing pattern — inject HTTP base URL from config
- Handle controller restart: if `lastRunTime` exists and `nextRunTime` is in the past, skip to the next future occurrence (no retroactive runs)

**Test requirements:**
- Unit test: cron parsing and next-run calculation (table-driven)
- Unit test: reconcile logic with mock HTTP client — schedule due, not due, agent missing, API failure
- Unit test: status update correctness (lastRunTime, nextRunTime, sessionID, result)
- Unit test: controller restart recovery (missed runs not retroactively executed)

**Integration notes:**
- Controller needs HTTP base URL config (e.g., `http://kagent-controller.kagent.svc:8080`)
- User ID for API calls: use a system user like `"system:cronjob@kagent.dev"`

**Demo:** Apply an AgentCronJob with `"*/2 * * * *"` schedule. Observe status updates every 2 minutes: `kubectl get agentcronjob -w`. Verify sessions appear in database.

---

## Step 3: HTTP Server CRUD Endpoints

**Objective:** Add REST endpoints so the UI (and kubectl proxy users) can manage AgentCronJob CRs.

**Implementation guidance:**
- Create `go/internal/httpserver/handlers/cronjobs.go`:
  - `CronJobHandler` struct with K8s `client.Client`
  - `HandleListCronJobs` — GET `/api/cronjobs` → `client.List(ctx, &v1alpha2.AgentCronJobList{}, ...)`
  - `HandleGetCronJob` — GET `/api/cronjobs/{namespace}/{name}` → `client.Get(ctx, ...)`
  - `HandleCreateCronJob` — POST `/api/cronjobs` → decode body, `client.Create(ctx, ...)`
  - `HandleUpdateCronJob` — PUT `/api/cronjobs/{namespace}/{name}` → decode body, `client.Update(ctx, ...)`
  - `HandleDeleteCronJob` — DELETE `/api/cronjobs/{namespace}/{name}` → `client.Delete(ctx, ...)`
- Register routes in `go/internal/httpserver/server.go` — add to router with auth middleware
- Response format: `StandardResponse[T]` (same as agents)
- Create request body mirrors CRD spec with metadata:
  ```go
  type CronJobRequest struct {
      Name      string `json:"name"`
      Namespace string `json:"namespace"`
      Schedule  string `json:"schedule"`
      Prompt    string `json:"prompt"`
      AgentRef  string `json:"agentRef"`
  }
  ```

**Test requirements:**
- Unit test each handler with mock K8s client (create, get, list, update, delete)
- Test error cases: not found, invalid input, conflict
- Test auth middleware is applied

**Integration notes:**
- Follows exact same pattern as existing agent handlers
- Auth middleware reuses existing `AuthnMiddleware`

**Demo:** `curl` the endpoints to create, list, and delete an AgentCronJob. Verify CR appears in `kubectl get agentcronjobs`.

---

## Step 4: UI List Page and Server Actions

**Objective:** Replace the "Coming soon" placeholder with a functional cron jobs list page.

**Implementation guidance:**
- Add TypeScript types to `ui/src/types/index.ts`:
  - `AgentCronJob`, `AgentCronJobSpec`, `AgentCronJobStatus` interfaces
- Create server actions `ui/src/app/actions/cronjobs.ts`:
  - `getCronJobs()` → `fetchApi<BaseResponse<AgentCronJob[]>>("/cronjobs")`
  - `getCronJob(namespace, name)` → `fetchApi("/cronjobs/{ns}/{name}")`
  - `deleteCronJob(namespace, name)` → `fetchApi("/cronjobs/{ns}/{name}", { method: "DELETE" })`
- Replace `ui/src/app/cronjobs/page.tsx` with list component:
  - Fetch cron jobs on mount via server action
  - Table layout with columns: Name, Schedule, Agent, Last Run, Next Run, Status
  - Expandable rows showing: prompt text, last run message, session link
  - Create button → `/cronjobs/new`
  - Edit button → `/cronjobs/new?edit=true&name=X&namespace=Y`
  - Delete button with confirmation dialog
  - Loading/error/empty states
  - Toast notifications for actions

**Test requirements:**
- Verify page renders with mock data
- Verify CRUD actions call correct API endpoints
- Verify error states display properly

**Integration notes:**
- Session link in expanded row: link to `/agents/{namespace}/{agentName}/chat?session={sessionID}` (or wherever sessions are viewable)
- Status badge: green for Success, red for Failed, gray for Pending

**Demo:** Navigate to `/cronjobs`, see list of cron jobs with status. Delete one, see toast confirmation.

---

## Step 5: UI Create/Edit Form

**Objective:** Add a form page for creating and editing AgentCronJob resources.

**Implementation guidance:**
- Create server actions in `ui/src/app/actions/cronjobs.ts`:
  - `createCronJob(data)` → `fetchApi("/cronjobs", { method: "POST", body })`
  - `updateCronJob(namespace, name, data)` → `fetchApi("/cronjobs/{ns}/{name}", { method: "PUT", body })`
- Create `ui/src/app/cronjobs/new/page.tsx`:
  - Form fields:
    - Name (text input, disabled in edit mode)
    - Namespace (text input or dropdown, disabled in edit mode)
    - Schedule (text input with cron expression, helper text showing human-readable translation)
    - Agent (dropdown populated from `GET /api/agents`)
    - Prompt (textarea, multi-line)
  - Edit mode: read query params `?edit=true&name=X&namespace=Y`, fetch existing CronJob, pre-populate form
  - Validation: all fields required, basic cron format check
  - Submit: call create or update action, redirect to `/cronjobs` on success
  - Cancel: navigate back to `/cronjobs`

**Test requirements:**
- Verify form renders in create and edit modes
- Verify validation prevents empty fields
- Verify submit calls correct action (create vs update)

**Integration notes:**
- Agent dropdown reuses existing agent list fetching
- Consider adding a "cron expression helper" — show next 3 run times as preview

**Demo:** Click "Create", fill out form with `*/5 * * * *` schedule, select agent, enter prompt, submit. See new cron job in list.

---

## Step 6: E2E Tests

**Objective:** Verify the full flow from CRD creation to scheduled execution.

**Implementation guidance:**
- Add tests in `go/test/e2e/agentcronjob_test.go`:
  - **Test: Create and verify status** — apply AgentCronJob, verify `Accepted=True` and `nextRunTime` is set
  - **Test: Scheduled execution** — use a very short schedule (`*/1 * * * *`), wait for execution, verify `lastRunTime` and `lastSessionID` are populated, verify session exists via API
  - **Test: Invalid agent ref** — apply AgentCronJob with non-existent agent, wait for scheduled time, verify `lastRunResult=Failed`
  - **Test: CRUD via API** — create/read/update/delete via HTTP endpoints, verify K8s state matches
  - **Test: Delete cleanup** — delete AgentCronJob, verify controller stops scheduling

**Test requirements:**
- Use existing E2E test framework and helpers from `go/test/e2e/`
- Tests require Kind cluster with kagent deployed
- Use a test agent (mock or simple echo agent)

**Integration notes:**
- E2E tests may need a longer timeout for cron-based tests (at least 2 minutes for `*/1` schedule)
- Consider using a mock HTTP server or test agent that responds immediately

**Demo:** `make -C go test-e2e` passes with new AgentCronJob tests.

---

## Step 7: Helm Chart and RBAC Updates

**Objective:** Package the CRD and controller RBAC for deployment.

**Implementation guidance:**
- Add CRD YAML to `helm/kagent-crds/templates/`:
  - Copy generated `config/crd/bases/kagent.dev_agentcronjobs.yaml` into chart
- Update `helm/kagent/templates/` RBAC:
  - Run `make -C go generate` to regenerate RBAC from markers
  - Verify ClusterRole includes agentcronjobs permissions
- Add sample values if any controller config is needed (e.g., HTTP base URL is likely already configured)
- Create example manifest `examples/agentcronjob.yaml`:
  ```yaml
  apiVersion: kagent.dev/v1alpha2
  kind: AgentCronJob
  metadata:
    name: daily-cluster-check
    namespace: default
  spec:
    schedule: "0 9 * * *"
    agentRef: "default/k8s-agent"
    prompt: "Check the health of all pods in the cluster and report any issues."
  ```

**Test requirements:**
- `helm lint helm/kagent-crds` passes
- `helm lint helm/kagent` passes
- `helm template test helm/kagent-crds` includes CRD
- Deploy to Kind cluster and verify CRD is available

**Integration notes:**
- CRD chart must be installed before main chart (existing pattern)
- No new Helm values needed for minimal implementation

**Demo:** `make helm-install` deploys kagent with AgentCronJob support. Apply example manifest, see it in UI.
