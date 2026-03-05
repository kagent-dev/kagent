# UI Cron Jobs Placeholder

## Current State
`ui/src/app/cronjobs/page.tsx` — minimal "Coming soon" placeholder with Clock icon.

Navigation already wired: sidebar has "Cron Jobs → /cronjobs" link.

## CRUD Page Patterns in Codebase

### Models Page (best fit for cron jobs)
- Inline state management, expandable rows
- Edit via `/models/new?edit=true&name=X&namespace=Y`
- Delete with confirmation dialog + toast

### Agent Page
- Delegates to `AgentList` component
- Card grid layout
- Uses context provider for state

## API Client Pattern
Server Actions in `ui/src/app/actions/`:
```typescript
export async function fetchApi<T>(path: string, options?): Promise<T>
```
- All requests include `user_id` query param
- Returns `BaseResponse<T> { message, data?, error? }`

## Types Pattern (ui/src/types/index.ts)
```typescript
export interface ResourceMetadata { name: string; namespace?: string; }
```

## Expected Backend Endpoints
```
GET    /api/cronjobs
GET    /api/cronjobs/{namespace}/{name}
POST   /api/cronjobs
PUT    /api/cronjobs/{namespace}/{name}
DELETE /api/cronjobs/{namespace}/{name}
```

## Components Used
Shadcn/UI: Button, Card, Dialog, Table, Badge, Input, ScrollArea, Tooltip
Icons: lucide-react (Clock, Plus, Pencil, Trash2, etc.)
Notifications: sonner toast
