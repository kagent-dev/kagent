# UI Structure & Routing Research

## Current Page Structure (Next.js App Router)

### Routes
| Route | Status | Description |
|-------|--------|-------------|
| `/` | AgentList (not dashboard) | Currently renders AgentGrid, no metrics |
| `/feed` | Placeholder | "Coming soon" |
| `/plugins` | Active | Plugin status page |
| `/agents` | Active | Agent list with create/edit |
| `/agents/[ns]/[name]/chat` | Active | Chat interface with A2A streaming |
| `/workflows` | Placeholder | "Coming soon" |
| `/cronjobs` | Active | Cron job management |
| `/models` | Active | Model config management |
| `/tools` | Active | Tool library (searchable, filterable) |
| `/servers` | Active | MCP server management |
| `/git` | Active | Git repo management |
| `/admin/org` | Placeholder | "Coming soon" |
| `/admin/gateways` | Placeholder | "Coming soon" |

### Layout Hierarchy
```
RootLayout
  TooltipProvider > AgentsProvider > NamespaceProvider > ThemeProvider
    AppInitializer > SidebarProvider
      AppSidebar (left sidebar)
        SidebarHeader (logo, theme toggle, namespace selector)
        SidebarContent > AppSidebarNav
        SidebarFooter > StatusIndicator
        SidebarRail (collapse toggle)
      SidebarInset (main content)
        MobileTopBar
        {children}
    Toaster (sonner)
```

### Key Files
- `ui/src/app/page.tsx` — root page (currently AgentList, needs to become Dashboard)
- `ui/src/app/layout.tsx` — root layout with providers
- `ui/src/components/AgentList.tsx` — current home page component
- `ui/src/components/AgentGrid.tsx` — agent card grid

## Sidebar Navigation

**NAV_SECTIONS** in `AppSidebarNav.tsx`:
- **OVERVIEW**: Dashboard (`/`), Live Feed (`/feed`), Plugins (`/plugins`)
- **AGENTS**: My Agents (`/agents`), Workflows (`/workflows`), Cron Jobs (`/cronjobs`)
- **RESOURCES**: Models (`/models`), Tools (`/tools`), MCP Servers (`/servers`), GIT Repos (`/git`)
- **ADMIN**: Organization (`/admin/org`), Gateways (`/admin/gateways`)

Dashboard nav item already exists pointing to `/`. Just need the actual dashboard page.

### Plugin Integration
- Plugins injected into nav dynamically via `SidebarStatusProvider`
- Badge updates via custom events: `kagent:plugin-badge`
- Unknown section names create a new "PLUGINS" section

## Implication for Dashboard
The current `/` page just shows AgentList. To add Dashboard:
1. Replace `page.tsx` at root with Dashboard component
2. Move AgentList to `/agents` route (or keep as sub-component)
3. Dashboard nav item already wired to `/`
