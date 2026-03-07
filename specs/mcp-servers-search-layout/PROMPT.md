# MCP Servers Page: Add Search & Stretch Layout

## Objective

Update `ui/src/app/servers/page.tsx` to match the Tools page (`ui/src/app/tools/page.tsx`) UX by adding a search bar with client-side filtering and a viewport-filling ScrollArea layout.

## Key Requirements

- Add search input (with Search icon) that filters servers by `ref`, `discoveredTools[].name`, and `discoveredTools[].description`
- Wrap server list in `ScrollArea` with `h-[calc(100vh-350px)]` for stretch layout
- Show result count ("N server(s) found") below search bar
- Auto-expand servers whose tools match the search term
- Highlight matching text using `<mark>` tags (same `highlightMatch` pattern as Tools page)
- Add "No servers found" empty state with "Clear Search" button when search yields no results
- Add `pb-12` to outer container
- Preserve all existing functionality: expand/collapse, add/delete server, "View Tools" link

## Acceptance Criteria

- Given the Servers page loads with servers, when the page renders, then a search input is visible above the list
- Given servers are loaded, when the user types in search, then the list filters in real-time by server name and tool names/descriptions
- Given a search matches tools inside a collapsed server, when the filter runs, then that server auto-expands
- Given a search term is active, when results render, then matching text is highlighted in yellow
- Given a search matches no servers, when the filter runs, then "No servers found" with "Clear Search" button is shown
- Given the server list is long, when the page renders, then the list scrolls within a ScrollArea
- Given search is active, when the user clicks "Add MCP Server", then the dialog works normally

## Reference

- Design: `specs/mcp-servers-search-layout/design.md`
- Plan: `specs/mcp-servers-search-layout/plan.md`
- Reference implementation: `ui/src/app/tools/page.tsx` (search, highlight, ScrollArea patterns)
