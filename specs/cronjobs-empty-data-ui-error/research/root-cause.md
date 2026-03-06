# Research: Root Cause Analysis

## The Bug

When no CronJobs exist, the UI shows an error instead of an empty state.

## Root Cause

`go/api/httpapi/types.go:27` - `StandardResponse.Data` has `omitempty`:
```go
Data    T      `json:"data,omitempty"`
```

When the Go handler returns a nil or empty slice, `omitempty` causes `data` to be omitted from JSON. The UI receives `{"error":false,"message":"..."}` (no `data` key). The UI then treats `!response.data` (undefined) as an error.

## Two Fix Strategies

### Option A: Backend fix (preferred)
Remove `omitempty` from `Data` field. Empty slices serialize as `"data":[]`.
- Single fix, addresses all endpoints at once
- More correct semantics: `data` should always be present in a success response

### Option B: Frontend fix
Change `!response.data` checks to treat missing data as empty array.
- Multiple files need changing
- Defensive but doesn't fix the root issue

### Option C: Both
Fix backend (Option A) + make frontend resilient (Option B) for defense in depth.

## Affected Pages (same `!response.data` pattern)

- `ui/src/app/cronjobs/page.tsx` (L53)
- `ui/src/app/git/page.tsx` (L65, L85)
- `ui/src/app/models/page.tsx` (L37)
- `ui/src/app/models/new/page.tsx` (L205, L298, L318, L488, L511)
- `ui/src/app/actions/plugins.ts` (L20)
