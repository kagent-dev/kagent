# Search Implementation & Shared Components

## Search Pattern (Tools Page)

- **Inline implementation** — no shared search component exists in the codebase
- Uses `useState<string>` for `searchTerm`, `<Input>` with Search icon
- Client-side filtering via `useMemo` across multiple fields:
  - Tool display name, description, server ref, tool internal name
- Case-insensitive matching
- `highlightMatch()` helper highlights search term in results

## CategoryFilter Component

- Location: `ui/src/components/tools/CategoryFilter.tsx`
- Props: `categories`, `selectedCategories`, toggle/selectAll/clearAll handlers
- Renders clickable Badge components + Select All / Clear All buttons
- Specific to Tools page but could be reused

## ToolServerResponse Type

```typescript
type ToolServerResponse = RemoteMCPServerResponse | MCPServerResponse;

interface RemoteMCPServerResponse {
  ref: string;           // namespace/name — displayed, sortable, searchable
  groupKind: string;     // K8s group/kind — available but not displayed
  discoveredTools: DiscoveredTool[];
}

interface DiscoveredTool {
  name: string;
  description: string;
}
```

### Searchable Fields for Servers Page

- `ref` — server namespace/name (already displayed)
- `discoveredTools[].name` — tool names within expanded servers
- `discoveredTools[].description` — tool descriptions
- Tool count (derived, for display)

## No Shared Layout Components

- Root layout provides sidebar, providers, toaster
- Each page defines its own container — no reusable page wrapper
