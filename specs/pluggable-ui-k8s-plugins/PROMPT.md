# Implement Left Navigation Sidebar for KAgent UI

## Objective

Replace the current top-only `Header` navigation in the KAgent Next.js UI with a persistent left sidebar that is visible on every page. Build exclusively on the existing shadcn/ui primitives in `components/ui/sidebar.tsx`. Do not introduce new UI libraries.

Full design and implementation plan: `specs/pluggable-ui-k8s-plugins/`

---

## Implement the 8 steps in `specs/pluggable-ui-k8s-plugins/plan.md` in order.

Each step must pass its tests before proceeding to the next.

---

## Key Requirements

- **Global sidebar**: `SidebarProvider` + `AppSidebar` + `SidebarInset` wired in `app/layout.tsx`; `Header` and `Footer` removed
- **Nav sections** (OVERVIEW, AGENTS, RESOURCES, ADMIN) with icons, labels, and routes per `specs/pluggable-ui-k8s-plugins/design.md` §Navigation Structure
- **Active state**: `isActive` + `aria-current="page"` on the item matching `usePathname()`
- **Collapsible**: `collapsible="icon"` mode; state persisted via existing `sidebar_state` cookie
- **NamespaceProvider**: new `lib/namespace-context.tsx` context; `NamespaceSelector` in sidebar header wraps existing `NamespaceCombobox` logic
- **Chat layout fix**: remove inner `SidebarProvider` from chat layout; `SessionsSidebar` → `side="right"`; `AgentDetailsSidebar` → `Sheet` with `open`/`onClose` props
- **Mobile**: `MobileTopBar` with `SidebarTrigger` (hamburger), hidden on `lg:` and above
- **Placeholder pages**: stub "Coming Soon" pages for `/feed`, `/workflows`, `/cronjobs`, `/kanban`, `/git`, `/admin/org`, `/admin/gateways`
- **Accessibility**: `aria-label="Main navigation"`, `aria-current="page"`, `role="group"` on section groups, zero axe-core critical violations

---

## Acceptance Criteria

```gherkin
Given I navigate to any page (/, /agents, /models, /tools, /servers)
Then a left sidebar is visible with the KAgent logo, namespace selector, and nav sections

Given I am on /agents
Then the "My Agents" nav item has isActive styling and aria-current="page"
And no other item has that state

Given the sidebar is expanded and I click the SidebarRail toggle
Then the sidebar collapses to icon-only (48px) with tooltips on hover
And on next page load the collapsed state is preserved (cookie)

Given a mobile viewport (<1024px)
Then the sidebar is hidden and a top bar with a hamburger button is visible
When I tap the hamburger
Then the sidebar opens as a full-height sheet overlay

Given I am on a chat page (/agents/[namespace]/[name]/chat/...)
Then the global left AppSidebar is visible
And a SessionsSidebar panel is on the right
And clicking the agent-info trigger opens AgentDetailsSidebar as a Sheet

Given I click any placeholder nav item (/feed, /workflows, /kanban, etc.)
Then I see a "Coming Soon" page with no 404 error
And the corresponding nav item is active in the sidebar

Given the sidebar is rendered
Then axe-core reports zero critical or serious accessibility violations
```

---

## File Map (create / modify)

| Action | Path |
|--------|------|
| CREATE | `ui/src/lib/namespace-context.tsx` |
| CREATE | `ui/src/components/sidebars/AppSidebar.tsx` |
| CREATE | `ui/src/components/sidebars/AppSidebarNav.tsx` |
| CREATE | `ui/src/components/sidebars/NamespaceSelector.tsx` |
| CREATE | `ui/src/components/sidebars/StatusIndicator.tsx` |
| CREATE | `ui/src/components/MobileTopBar.tsx` |
| CREATE | `ui/src/app/feed/page.tsx` |
| CREATE | `ui/src/app/workflows/page.tsx` |
| CREATE | `ui/src/app/cronjobs/page.tsx` |
| CREATE | `ui/src/app/kanban/page.tsx` |
| CREATE | `ui/src/app/git/page.tsx` |
| CREATE | `ui/src/app/admin/org/page.tsx` |
| CREATE | `ui/src/app/admin/gateways/page.tsx` |
| MODIFY | `ui/src/app/layout.tsx` |
| MODIFY | `ui/src/app/agents/[namespace]/[name]/chat/layout.tsx` |
| MODIFY | `ui/src/components/chat/ChatLayoutUI.tsx` |
| MODIFY | `ui/src/components/sidebars/SessionsSidebar.tsx` |
| MODIFY | `ui/src/components/sidebars/AgentDetailsSidebar.tsx` |
| DELETE | `ui/src/components/Header.tsx` *(after migration verified)* |
| DELETE | `ui/src/components/Footer.tsx` *(after migration verified)* |
