# Research: CronJob UI Components

## Key Files

| File | Purpose |
|------|---------|
| `ui/src/app/cronjobs/page.tsx` | Main list view - renders table of all cron jobs |
| `ui/src/app/cronjobs/new/page.tsx` | Create/Edit form |
| `ui/src/app/actions/cronjobs.ts` | API action functions (getCronJobs, createCronJob, etc.) |
| `ui/src/types/index.ts` (L438-467) | TypeScript interfaces |
| `ui/src/components/sidebars/AppSidebarNav.tsx` (L80) | Nav link to /cronjobs |

## Data Flow

1. `getCronJobs()` calls `fetchApi<BaseResponse<AgentCronJob[]>>("/cronjobs")`
2. Response checked: `if (response.error || !response.data)` -> throw
3. Data set via `setCronJobs(response.data)`
4. Empty list: renders "No cron jobs found" placeholder

## Empty/Null Handling

- **Empty list**: Shows placeholder with Clock icon and message (L126-130)
- **Optional chaining**: `job.status?.nextRunTime`, `job.status?.lastRunTime`, etc.
- **formatTime()**: Returns "N/A" for falsy/invalid timestamps
- **lastResult**: Falls back to "N/A" when undefined
- **Status is optional**: `AgentCronJob.status?: AgentCronJobStatus`

## Error Handling Pattern

```typescript
// API layer
export async function getCronJobs(): Promise<BaseResponse<AgentCronJob[]>> {
  try {
    const response = await fetchApi<BaseResponse<AgentCronJob[]>>("/cronjobs");
    if (!response) throw new Error("Failed to get cron jobs");
    response.data?.sort(...);
    return { message: "...", data: response.data };
  } catch (error) {
    return createErrorResponse<AgentCronJob[]>(error, "Error getting cron jobs");
  }
}

// Component layer
const response = await getCronJobs();
if (response.error || !response.data) {
  throw new Error(response.error || "Failed to fetch cron jobs");
}
setCronJobs(response.data);
```
