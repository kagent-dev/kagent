# UI Patterns (Next.js)

## Reference: AgentCronJob UI (most recent feature)

## Page Structure
- List page: `/app/cronjobs/page.tsx` — "use client", state, useEffect fetch
- Create/Edit: `/app/cronjobs/new/page.tsx` — Suspense wrapper, useSearchParams for edit mode
- Edit via query params: `/cronjobs/new?edit=true&name=x&namespace=y`

## Server Actions (`/app/actions/cronjobs.ts`)
```typescript
"use server";
export async function getCronJobs(): Promise<BaseResponse<AgentCronJob[]>> {
  const response = await fetchApi<BaseResponse<AgentCronJob[]>>("/cronjobs");
  return { message: "...", data: response.data };
}
```
- `fetchApi<T>()` adds user_id, 15s timeout, Content-Type header
- `createErrorResponse<T>()` for error handling
- `revalidatePath()` after mutations

## List Page Pattern
- useState for data, loading, error, expandedRows
- useEffect → server action → setState
- LoadingState / ErrorState components for states
- Expandable rows with chevron icons
- Delete with Dialog confirmation + toast
- Table with inline sorting

## Create/Edit Form Pattern
- useSearchParams for edit detection
- useState per field + validation errors object
- validateForm() → returns boolean, sets error state
- Submit → server action → router.push back to list
- Disabled name/namespace fields in edit mode

## Component Library
- Shadcn UI: Button, Dialog, Input, Textarea, Label, Select
- Lucide icons: Plus, Pencil, Trash2, ChevronDown, ChevronRight
- Custom: NamespaceCombobox, LoadingState, ErrorState
- TailwindCSS for styling

## Types (`/ui/src/types/index.ts`)
- `BaseResponse<T>` — standard API response wrapper
- `ResourceMetadata` — K8s metadata (name, namespace)
- Entity interfaces mirror backend responses

## Key Files
| File | Purpose |
|------|---------|
| `/app/cronjobs/page.tsx` | List page reference (274 lines) |
| `/app/cronjobs/new/page.tsx` | Form reference (306 lines) |
| `/app/actions/cronjobs.ts` | Server actions reference |
| `/app/actions/utils.ts` | fetchApi, error helpers |
| `/types/index.ts` | All TypeScript types |
