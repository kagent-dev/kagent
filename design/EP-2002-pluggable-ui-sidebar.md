# EP-2002: Pluggable UI Left Sidebar

* Status: **Implemented**
* Spec: [specs/pluggable-ui-k8s-plugins](../specs/pluggable-ui-k8s-plugins/)

## Background

Replace the KAgent top-nav Header with a persistent left sidebar built on shadcn/ui primitives. Provides grouped navigation, Kubernetes namespace selector, and plugin discovery.

## Motivation

The top navigation bar doesn't scale with growing number of pages and plugins. A left sidebar provides grouped sections, collapse-to-icons mode, and dynamic plugin entries.

### Goals

- Grouped navigation: OVERVIEW / AGENTS / RESOURCES / ADMIN sections
- Kubernetes namespace selector in sidebar header
- Collapse-to-icons mode and mobile sheet overlay
- Dynamic plugin entries discovered via `/api/plugins`

### Non-Goals

- Multi-level nested navigation
- Drag-and-drop sidebar customization

## Implementation Details

- **Components:** `AppSidebar`, `SidebarProvider`, `SidebarInset` from shadcn/ui
- **Layout:** Chat's `SessionsSidebar` moved to `side="right"`, `AgentDetailsSidebar` becomes a `Sheet`
- **Files:** `ui/src/components/sidebars/AppSidebar.tsx`, `ui/src/app/layout.tsx`

### Test Plan

- Visual regression testing
- Mobile responsive layout verification
