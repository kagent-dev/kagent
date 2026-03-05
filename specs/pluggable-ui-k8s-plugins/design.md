# Design: Left Navigation Sidebar for KAgent UI

## Overview

Replace the current top-only `Header` navigation with a persistent vertical left sidebar that serves as the primary navigation for the entire KAgent web application. The sidebar provides grouped, hierarchical access to all major sections, a Kubernetes namespace selector, and a status/profile footer. It is built exclusively on the existing shadcn/ui sidebar primitives already present in the codebase.

---

## Detailed Requirements

*(Consolidated from `requirements.md`)*

### Functional
- Sidebar rendered in root layout — visible on every page
- Current `Header` removed; optionally replaced with a minimal top breadcrumb bar
- Active route highlighted (blue background, bold text)
- Section labels (OVERVIEW, AGENTS, RESOURCES, ADMIN) are non-clickable group headers
- Collapsible (icons-only) on desktop, persisted via `sidebar_state` cookie
- Mobile: hidden by default, opens as Sheet overlay via hamburger button
- Keyboard navigation: Tab/Arrow keys to focus items, Enter/Space to activate
- Namespace selector dropdown filters all views

### Navigation Structure

| Section | Item | Route | Status |
|---------|------|-------|--------|
| OVERVIEW | Dashboard | `/` | existing |
| | Live Feed | `/feed` | placeholder |
| AGENTS | My Agents | `/agents` | existing |
| | Workflows | `/workflows` | placeholder |
| | Cron Jobs | `/cronjobs` | placeholder |
| | Kanban | `/kanban` | placeholder |
| RESOURCES | Models | `/models` | existing |
| | Tools | `/tools` | existing |
| | MCP Servers | `/servers` | existing |
| | GIT Repos | `/git` | placeholder |
| ADMIN | Organization | `/admin/org` | placeholder |
| | Gateways | `/admin/gateways` | placeholder |

### Technical
- Build on `components/ui/sidebar.tsx` primitives only (no new libraries)
- New component structure under `components/sidebars/`
- Root layout wraps with `SidebarProvider` + `SidebarInset`
- Chat layout's `SessionsSidebar` becomes a right-side secondary sidebar
- New `NamespaceProvider` context for global namespace state
- Routing via Next.js `<Link>` + `usePathname()` for active state

### Non-Functional
- Expanded width: 240px; collapsed width: 48px (icon-only)
- Collapse animation: 200ms ease-in-out
- No layout shift on load (cookie read server-side via shadcn default)
- WCAG 2.1 AA accessibility

---

## Architecture Overview

```mermaid
graph TD
    A[app/layout.tsx<br/>Root Layout] --> B[SidebarProvider<br/>global state + cookie]
    B --> C[AppSidebar<br/>components/sidebars/AppSidebar.tsx]
    B --> D[SidebarInset<br/>main content area]
    C --> E[SidebarHeader<br/>Logo + NamespaceSelector]
    C --> F[SidebarContent<br/>AppSidebarNav]
    C --> G[SidebarFooter<br/>StatusIndicator + ThemeToggle]
    F --> H[NavSection: OVERVIEW]
    F --> I[NavSection: AGENTS]
    F --> J[NavSection: RESOURCES]
    F --> K[NavSection: ADMIN]
    D --> L[{children}<br/>Page content]
    D --> M[Chat Route only:<br/>SessionsSidebar side=right]

    N[NamespaceProvider<br/>app/layout.tsx] --> C
    N --> L
```

### Layout Nesting (Chat Route)

```
Root layout (SidebarProvider global)
  └── SidebarInset
        └── Chat layout (SidebarProvider chat — overrides inner context)
              ├── SessionsSidebar  side="right"  ← CHANGED from left to right
              ├── main content
              └── AgentDetailsSidebar  side="right"  ← PROBLEM: two right sidebars
```

> **Resolution:** `AgentDetailsSidebar` is converted to a `Sheet` (overlay panel) triggered by a button in the chat interface, removing it from the sidebar primitive flow. `SessionsSidebar` moves to `side="right"`.

---

## Components and Interfaces

### `components/sidebars/AppSidebar.tsx`
```tsx
// Main application sidebar — wraps all sidebar sections
export function AppSidebar() {
  const { namespace, setNamespace } = useNamespace();
  return (
    <Sidebar collapsible="icon">
      <SidebarHeader>
        <Logo />
        <NamespaceSelector value={namespace} onValueChange={setNamespace} />
      </SidebarHeader>
      <SidebarContent>
        <AppSidebarNav />
      </SidebarContent>
      <SidebarFooter>
        <StatusIndicator />
        <ThemeToggle />
      </SidebarFooter>
      <SidebarRail />
    </Sidebar>
  );
}
```

### `components/sidebars/AppSidebarNav.tsx`
```tsx
// Navigation sections with grouped items
const NAV_SECTIONS: NavSection[] = [
  {
    label: "OVERVIEW",
    items: [
      { label: "Dashboard", href: "/", icon: LayoutDashboard },
      { label: "Live Feed", href: "/feed", icon: Activity },
    ],
  },
  {
    label: "AGENTS",
    items: [
      { label: "My Agents", href: "/agents", icon: Bot },
      { label: "Workflows", href: "/workflows", icon: GitBranch },
      { label: "Cron Jobs", href: "/cronjobs", icon: Clock },
      { label: "Kanban", href: "/kanban", icon: LayoutKanban },
    ],
  },
  // RESOURCES, ADMIN ...
];

export function AppSidebarNav() {
  const pathname = usePathname();
  return (
    <>
      {NAV_SECTIONS.map((section) => (
        <SidebarGroup key={section.label}>
          <SidebarGroupLabel>{section.label}</SidebarGroupLabel>
          <SidebarMenu>
            {section.items.map((item) => (
              <SidebarMenuItem key={item.href}>
                <SidebarMenuButton asChild isActive={pathname === item.href}>
                  <Link href={item.href}>
                    <item.icon />
                    <span>{item.label}</span>
                  </Link>
                </SidebarMenuButton>
              </SidebarMenuItem>
            ))}
          </SidebarMenu>
        </SidebarGroup>
      ))}
    </>
  );
}
```

### `components/sidebars/NamespaceSelector.tsx`
Thin wrapper around existing `NamespaceCombobox` styled for sidebar context (compact variant, full-width).

### `components/sidebars/StatusIndicator.tsx`
```tsx
// Shows health status from /api/health or static
export function StatusIndicator() {
  return (
    <div className="flex items-center gap-2 px-2 py-1 text-xs text-muted-foreground">
      <span className="h-2 w-2 rounded-full bg-green-500" />
      All systems operational
    </div>
  );
}
```

### `lib/namespace-context.tsx` (new)
```tsx
interface NamespaceContextType {
  namespace: string;
  setNamespace: (ns: string) => void;
}

export const NamespaceContext = createContext<NamespaceContextType>(...);
export function NamespaceProvider({ children }: { children: ReactNode }) { ... }
export function useNamespace() { ... }
```

---

## Data Models

No new backend data models required. The sidebar is purely a UI navigation concern.

**State managed client-side:**
- `sidebar_state` cookie: `"true"` (expanded) | `"false"` (collapsed) — managed by existing shadcn `SidebarProvider`
- `namespace`: string — managed by new `NamespaceProvider`, initialized from `NamespaceCombobox` logic (prefers "kagent" > "default" > first available)

---

## Layout Changes

### `app/layout.tsx` (modified)
```tsx
export default function RootLayout({ children }) {
  return (
    <TooltipProvider>
      <AgentsProvider>
        <NamespaceProvider>          {/* NEW */}
          <html lang="en">
            <body className="...">
              <ThemeProvider ...>
                <AppInitializer>
                  <SidebarProvider>  {/* NEW — replaces Header */}
                    <AppSidebar />   {/* NEW */}
                    <SidebarInset>   {/* NEW — replaces <main> */}
                      {children}
                    </SidebarInset>
                  </SidebarProvider>
                  {/* Header REMOVED */}
                  {/* Footer REMOVED or moved into AppSidebar footer */}
                </AppInitializer>
                <Toaster richColors />
              </ThemeProvider>
            </body>
          </html>
        </NamespaceProvider>
      </AgentsProvider>
    </TooltipProvider>
  );
}
```

### `app/agents/[namespace]/[name]/chat/layout.tsx` (modified)
- Remove outer `SidebarProvider` (global one is sufficient)
- Change `SessionsSidebar` to `side="right"`
- Convert `AgentDetailsSidebar` to a `Sheet` triggered from a button

### `components/chat/ChatLayoutUI.tsx` (modified)
```tsx
// Remove SessionsSidebar from here (or move to right side)
// Replace AgentDetailsSidebar with a Sheet trigger button
```

---

## Error Handling

- **Namespace load failure:** `NamespaceSelector` shows error state inline; sidebar remains functional with last-known namespace
- **Navigation to placeholder routes:** Render empty page with "Coming Soon" message; do not block routing
- **Sidebar cookie missing/corrupt:** `SidebarProvider` defaults to `defaultOpen=true`

---

## Acceptance Criteria

```gherkin
Feature: Left Navigation Sidebar

Scenario: Sidebar visible on all pages
  Given I navigate to any page in KAgent (/, /agents, /models, /tools, /servers)
  Then I see a left sidebar with the KAgent logo and navigation sections

Scenario: Active route highlighting
  Given I am on the /agents page
  When the sidebar renders
  Then the "My Agents" nav item has an active visual state (highlighted background)
  And no other nav item is highlighted

Scenario: Sidebar collapses to icon mode
  Given the sidebar is expanded
  When I click the SidebarRail or SidebarTrigger
  Then the sidebar collapses to icon-only mode (48px wide)
  And labels are hidden, only icons remain visible
  And a tooltip shows the label on hover

Scenario: Collapsed state persists across page navigation
  Given I have collapsed the sidebar
  When I navigate to a different page
  Then the sidebar remains collapsed

Scenario: Mobile sidebar as overlay
  Given I am on a mobile viewport (<1024px)
  Then the sidebar is hidden
  When I tap the hamburger button
  Then the sidebar opens as a full-height sheet overlay

Scenario: Namespace selector filters views
  Given I open the namespace selector
  When I select namespace "production"
  Then all views (agents, models, tools, servers) load resources from "production"

Scenario: Keyboard navigation
  Given the sidebar is visible
  When I press Tab to focus a nav item and Enter to activate
  Then I navigate to the corresponding route

Scenario: Chat layout coexists
  Given I navigate to a chat page
  Then the global left sidebar remains visible
  And the SessionsSidebar appears on the RIGHT side
  And I can toggle either sidebar independently
```

---

## Testing Strategy

- **Unit tests:** `AppSidebarNav` renders correct items; active state applies for matching pathname; `NamespaceProvider` propagates value
- **Integration tests:** Root layout renders `AppSidebar` + `SidebarInset` together; chat layout renders both sidebars without conflict
- **E2E tests:** Navigate between pages and verify sidebar active state; collapse/expand cycle; mobile sheet behavior; namespace switch propagation
- **Visual regression:** Snapshot tests for expanded and collapsed states

---

## Appendix A: Technology Choices

| Concern | Choice | Rationale |
|---------|--------|-----------|
| Sidebar primitives | shadcn/ui `sidebar.tsx` | Already in codebase; no new dependency |
| State persistence | `sidebar_state` cookie | Already implemented in shadcn provider |
| Namespace state | React Context (`NamespaceProvider`) | Lightweight; fits existing pattern of `AgentsProvider` |
| Routing | Next.js `<Link>` + `usePathname()` | Standard Next.js App Router pattern |
| Icons | lucide-react | Already used throughout codebase |

## Appendix B: Alternative Approaches Considered

**Alternative 1: Keep Header, add sidebar only for specific pages**
- Rejected: requirement is for sidebar on ALL pages; mixing header+sidebar is inconsistent UX

**Alternative 2: Use a completely new sidebar library (e.g., Radix Navigation)**
- Rejected: requirement explicitly forbids new sidebar libraries; shadcn primitives are sufficient

**Alternative 3: Move `AgentDetailsSidebar` to `side="left"` as well**
- Rejected: shadcn only supports one sidebar per `SidebarProvider` instance; multiple sidebars require nested providers

**Alternative 4: Nest a second `SidebarProvider` for chat layout**
- Viable: React context resolution means the inner provider overrides the outer for chat routes. The global AppSidebar would still render (it reads from the outer provider via direct ref, not context). **Complexity risk:** chosen approach (Sheet for AgentDetails) is simpler.

## Appendix C: Out of Scope

- Actual page content for placeholder routes (`/feed`, `/workflows`, `/kanban`, `/cronjobs`, `/git`, `/admin/*`)
- User profile/avatar in sidebar footer (mentioned in reference screenshot)
- CRD-based "pluggable" plugin system (the broader original idea — this spec implements the sidebar foundation that plugins would extend)
- Theming changes beyond sidebar styles
