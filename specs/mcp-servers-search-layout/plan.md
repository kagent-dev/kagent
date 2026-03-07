# Implementation Plan: MCP Servers Page — Search & Stretch Layout

## Checklist

- [ ] Step 1: Add search state, imports, and search bar UI
- [ ] Step 2: Add client-side filtering with useMemo
- [ ] Step 3: Add auto-expand on search and highlight helper
- [ ] Step 4: Wrap server list in ScrollArea and add result count
- [ ] Step 5: Add search empty state and update outer container

---

## Step 1: Add search state, imports, and search bar UI

**Objective:** Add the search input to the Servers page header.

**Implementation guidance:**
- Add `useMemo` to the React imports
- Add `Search` to the lucide-react imports
- Add `Input` import from `@/components/ui/input`
- Add `ScrollArea` import from `@/components/ui/scroll-area`
- Add `const [searchTerm, setSearchTerm] = useState<string>("")` state
- Insert search bar markup between the header and the server list (after the `</div>` closing the flex justify-between header, before the `isLoading` ternary)

**Test:** Page renders with search input visible above the server list. Typing in it updates the input value.

**Demo:** Search bar appears on the Servers page, accepts input.

---

## Step 2: Add client-side filtering with useMemo

**Objective:** Filter the server list based on the search term.

**Implementation guidance:**
- Add a `filteredServers` useMemo that filters `servers` by:
  - `server.ref` matching search term (case-insensitive)
  - Any `discoveredTools[].name` or `discoveredTools[].description` matching
- Replace `servers.map(...)` in the render with `filteredServers.map(...)`
- Update the "Add MCP Server" button visibility check from `servers.length > 0` to always show when `servers.length > 0` (regardless of filter)

**Test:** Typing a server name filters the list. Typing a tool name shows only servers containing that tool. Clearing search shows all servers.

**Demo:** Type a partial server name — list filters in real-time.

---

## Step 3: Add auto-expand on search and highlight helper

**Objective:** Auto-expand servers whose tools match the search, and highlight matched text.

**Implementation guidance:**
- Add a `useEffect` that watches `searchTerm` and `servers`: when a search term matches tools inside a server, add that server to `expandedServers`
- Add `highlightMatch(text, highlight)` helper function (same pattern as Tools page) that splits text on the search term and wraps matches in `<mark>` tags
- Apply `highlightMatch` to:
  - Server name (`server.ref`) in the header
  - Tool names and descriptions in the expanded tools grid

**Test:** Search for a tool name — the server containing it auto-expands and the matching text is highlighted in yellow.

**Demo:** Type a tool name — parent server expands, matching text highlighted.

---

## Step 4: Wrap server list in ScrollArea and add result count

**Objective:** Add viewport-filling scroll layout and server count display.

**Implementation guidance:**
- Add result count div between search bar and server list: `{filteredServers.length} server(s) found`
- Wrap the `<div className="space-y-4">` server list in `<ScrollArea className="h-[calc(100vh-350px)] pr-4 -mr-4">`
- Update outer container from `mt-12 mx-auto max-w-6xl px-6` to `mt-12 mx-auto max-w-6xl px-6 pb-12`

**Test:** With many servers, the list scrolls within the ScrollArea. The count updates as search filters results.

**Demo:** Server count displays correctly, long lists scroll within the viewport.

---

## Step 5: Add search empty state and update rendering logic

**Objective:** Handle the case where search yields no results.

**Implementation guidance:**
- Add a new conditional branch: when `filteredServers.length === 0 && servers.length > 0`, render a "No servers found" empty state with Server icon, message, and "Clear Search" button that calls `setSearchTerm("")`
- Rendering logic order: loading → no search results (new) → has results → no servers at all (existing)
- Ensure the "Add MCP Server" button in the header remains visible during search-no-results state

**Test:** Search for a nonsensical string — "No servers found" state appears with "Clear Search" button. Clicking it restores full list.

**Demo:** Search "zzzzz" — empty state shown. Click "Clear Search" — all servers reappear.
