# Implementation Plan: Fix CronJobs Empty Data UI Error

## Checklist

- [ ] Step 1: Fix backend handler (nil slice initialization)
- [ ] Step 2: Fix frontend fetch logic (remove false-positive error)
- [ ] Step 3: Add backend unit test
- [ ] Step 4: Verify end-to-end behavior

---

## Step 1: Fix backend handler

**Objective:** Ensure `HandleListCronJobs` always returns a non-nil slice so `data` is serialized as `[]` in JSON.

**Implementation guidance:**
- File: `go/core/internal/httpserver/handlers/cronjobs.go`
- After the `KubeClient.List` call (line 40), add a nil check on `cronJobList.Items`
- If nil, assign `[]v1alpha2.AgentCronJob{}`
- Pass the initialized slice to `api.NewResponse`

**Test requirements:**
- Compile succeeds (`go build ./...` from `go/core`)

**Integration notes:**
- No API contract change — response shape is identical, just `data` is now always present

**Demo:** `curl GET /api/cronjobs` on a cluster with no CronJobs returns `{"error":false,"data":[],"message":"Successfully listed AgentCronJobs"}`

---

## Step 2: Fix frontend fetch logic

**Objective:** Stop treating missing `data` as an error on the CronJobs page.

**Implementation guidance:**
- File: `ui/src/app/cronjobs/page.tsx`
- In `fetchCronJobs()` (line 53), change `if (response.error || !response.data)` to `if (response.error)`
- Change `setCronJobs(response.data)` to `setCronJobs(response.data ?? [])`

**Test requirements:**
- Existing test in `ui/src/app/__tests__/stub-pages.test.tsx` passes
- `npm run build` in `ui/` succeeds

**Integration notes:**
- Empty state UI (lines 126-130) becomes reachable when `cronJobs.length === 0`

**Demo:** Navigate to /cronjobs with no CronJobs — Clock icon and "No cron jobs found" message displayed instead of error.

---

## Step 3: Add backend unit test

**Objective:** Prevent regression by testing the empty list response.

**Implementation guidance:**
- File: `go/core/internal/httpserver/handlers/cronjobs_test.go` (new or existing)
- Test case: mock K8s client returning empty `AgentCronJobList`, call `HandleListCronJobs`, assert response body contains `"data":[]`
- Test case: mock K8s client returning populated list, assert `data` contains the items

**Test requirements:**
- `go test ./internal/httpserver/handlers/...` passes

**Integration notes:**
- Follow existing test patterns in the handlers directory

**Demo:** `go test -v -run TestHandleListCronJobs` shows both cases passing.

---

## Step 4: Verify end-to-end behavior

**Objective:** Confirm the fix works in a real environment.

**Implementation guidance:**
- Deploy to local Kind cluster (`make helm-install`)
- Ensure no AgentCronJob CRs exist
- Open /cronjobs in browser — verify empty state renders
- Create a CronJob via the UI — verify it appears in the list
- Delete the CronJob — verify empty state returns

**Test requirements:**
- All four acceptance criteria from design.md pass

**Integration notes:**
- No E2E test automation required (UI resilience fix, not new API/CRD)

**Demo:** Screenshot of empty state rendering correctly.
