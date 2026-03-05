# Requirements — Left Navigation Sidebar

This file documents the requirements for a persistent left-side navigation bar, inspired by the reference screenshot.

**Reference**: `specs/pluggable-ui-k8s-plugins/screenshot.png`

---

## 1. Overview

Replace the current top-only `Header` navigation with a vertical left sidebar that serves as the primary navigation for the entire application. The sidebar must be visible on every page (not only in chat views) and provide grouped, hierarchical access to all major sections of the UI.


---

## 2. Visual Structure (from reference screenshot)

The sidebar is a fixed-width vertical panel on the left edge of the viewport with the following top-to-bottom layout:

### 2.1 Brand / App Header
- App logo and name ("KAgent") at the top of the sidebar.
- A workspace/context selector dropdown immediately below (e.g., namespace selector).

### 2.2 Grouped Navigation Sections

Each section has an uppercase label and contains a list of navigation items with icons.

| Section          | Items                                      | Mapped KAgent Route         |
|------------------|--------------------------------------------|-----------------------------|
| **OVERVIEW**     | Dashboard                                  | `/`                         |
|                  | Live Feed                                  | `/feed` (new)               |
| **AGENTS**       | My Agents                                  | `/agents`                   |
|                  | Workflows                                  | `/workflows` (new)          |
|                  | Cron Jobs                                  | `/cronjobs`  (new)          |
|                  | Kanban                                     | `/kanban` (new)             |
| **RESOURCES**    | Models                                     | `/models`                   |
|                  | Tools                                      | `/tools`                    |
|                  | MCP Servers                                | `/servers`                  |
|                  | GIT Repos                                  | `/git`                      |
| **ADMIN**        | Organization                               | `/admin/org` (new)          |
|                  | Gateways                                   | `/admin/gateways` (new)     |

### 2.3 Footer Area
- Status indicator at the bottom (e.g., "All systems operational" with a green dot).
- User avatar / profile menu.

---

## 3. Functional Requirements

### 3.1 Always Visible
- The sidebar MUST be rendered in the root layout (`app/layout.tsx`) so it appears on every page, not just chat views.
- The current `Header` component should be removed or reduced to a minimal top bar (breadcrumb / page title only).

### 3.2 Active State
- The currently active route MUST be visually highlighted (e.g., blue background and bold text as shown in the screenshot).
- Parent section labels are not clickable; only leaf items are links.

### 3.3 Collapsible / Responsive
- On desktop (>=1024px): sidebar is expanded by default, showing icons + labels.
- The sidebar SHOULD support a collapsed mode (icons only) triggered by a rail/toggle control, persisted via cookie (reuse existing `sidebar_state` cookie from `sidebar.tsx`).
- On mobile (<1024px): sidebar is hidden by default and opens as a sheet/overlay triggered by a hamburger button.

### 3.4 Keyboard Navigation
- Sidebar items must be focusable and navigable via Tab / Arrow keys.
- Enter/Space activates the focused item.

### 3.5 Namespace Selector
- A dropdown at the top of the sidebar lets the user switch the active Kubernetes namespace context.
- Changing namespace filters all views (agents, models, tools, servers) to that namespace.

---

## 4. Technical Requirements

### 4.1 Reuse Existing Primitives
- Build on the existing shadcn/ui sidebar primitives already in `components/ui/sidebar.tsx` (`Sidebar`, `SidebarProvider`, `SidebarContent`, `SidebarHeader`, `SidebarFooter`, `SidebarMenu`, `SidebarMenuItem`, `SidebarMenuButton`, `SidebarRail`, etc.).
- Do NOT introduce a new sidebar library.

### 4.2 Component Structure
```
components/
  sidebars/
    AppSidebar.tsx          # New — main application sidebar
    AppSidebarNav.tsx        # New — navigation sections and items
    NamespaceSelector.tsx    # New — namespace dropdown
    StatusIndicator.tsx      # New — system status footer
```

### 4.3 Layout Changes
- `app/layout.tsx` must wrap children in `SidebarProvider` + `Sidebar` + `SidebarInset` so the sidebar is global.
- The chat-specific layout (`app/agents/[namespace]/[name]/chat/layout.tsx`) should nest its own `SessionsSidebar` inside the main content area (secondary sidebar), not replace the global one.

### 4.4 Routing
- Navigation items use Next.js `<Link>` for client-side transitions.
- Active state detection uses `usePathname()` from `next/navigation`.

### 4.5 Accessibility
- Sidebar landmark: `<nav aria-label="Main navigation">`.
- Section headers use `role="group"` with `aria-labelledby`.
- All interactive elements meet WCAG 2.1 AA contrast requirements.

---

## 5. Non-Functional Requirements

- Sidebar width: ~240px expanded, ~48px collapsed (consistent with screenshot proportions).
- Transition animation for collapse/expand: 200ms ease-in-out.
- No layout shift on page load — sidebar state is read from cookie server-side.
- Sidebar should not block or overlay main content on desktop.

---

## 6. Out of Scope

- The new routes listed as "(new)" in section 2.2 (e.g., `/feed`, `/workflows`, `/admin/*`) are placeholders. Only navigation links need to be wired; the actual page content is out of scope for this spec.
- Agent-to-agent (A2A) dashboard views.
- Theming changes beyond sidebar-specific styles.

---
