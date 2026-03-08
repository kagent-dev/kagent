# EP-2008: MCP Servers Page Search and Layout

* Status: **Implemented**
* Spec: [specs/mcp-servers-search-layout](../specs/mcp-servers-search-layout/)

## Background

Single-file enhancement to the MCP Servers page adding client-side search, auto-expand on match, and improved layout.

## Motivation

With many MCP servers and tools, users need to quickly find specific tools by name or description without manually expanding each server.

### Goals

- Search bar filtering by server name, tool name, and tool description
- Auto-expand servers when search matches their tools
- Search term highlighting in results
- ScrollArea for viewport-filling layout
- All servers expanded by default

### Non-Goals

- Server-side search
- Tool execution from search results

## Implementation Details

- **File:** `ui/src/app/servers/page.tsx` — single file modification
- **Search:** Client-side `useMemo` filter with `useState` for search term
- **Layout:** `ScrollArea` component for scroll containment

### Test Plan

- Manual testing with multiple MCP servers
- Search match highlighting verification
