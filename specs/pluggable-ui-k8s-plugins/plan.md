# Implementation Plan: Left Navigation Sidebar

## Checklist

- [ ] Step 1 — NamespaceProvider context
- [ ] Step 2 — AppSidebarNav + static AppSidebar component
- [ ] Step 3 — Root layout migration (wire sidebar globally, remove Header/Footer)
- [ ] Step 4 — NamespaceSelector in sidebar header
- [ ] Step 5 — Chat layout conflict resolution (SessionsSidebar right, AgentDetailsSidebar as Sheet)
- [ ] Step 6 — Mobile trigger bar
- [ ] Step 7 — StatusIndicator + sidebar footer polish + accessibility
- [ ] Step 8 — Placeholder route stub pages

---

## Step 1: NamespaceProvider context

**Objective:** Add a global React context that holds the active Kubernetes namespace and exposes it to any component in the tree. This is the data foundation that the sidebar's `NamespaceSelector` and all resource-listing pages will consume.

**Implementation guidance:**
- Create `ui/src/lib/namespace-context.tsx`
- Export `NamespaceContext`, `NamespaceProvider`, and `useNamespace()` hook
- State: `namespace: string`, `setNamespace: (ns: string) => void`
- Initialize namespace to `""` (empty); actual default selection happens inside `NamespaceSelector` when namespaces load (reuse existing logic from `NamespaceCombobox`: prefer "kagent" > "default" > first)
- Wrap `AgentsProvider` in root layout with `<NamespaceProvider>` — do NOT modify `app/layout.tsx` rendering yet (Header still present); just insert the provider wrapper
- Export `useNamespace` from an index or directly from `lib/namespace-context.tsx`

**Test requirements:**
- Unit test `ui/src/lib/__tests__/namespace-context.test.tsx`:
  - `useNamespace()` throws if used outside provider
  - `setNamespace("production")` updates context value
  - Provider renders children

**Integration notes:** No UI changes visible yet. Purely additive.

**Demo:** `useNamespace()` hook importable and usable in any client component.

---

## Step 2: AppSidebarNav + static AppSidebar component

**Objective:** Build the full sidebar component with all navigation sections and items using existing shadcn primitives. Active-route highlighting works. No namespace selector yet — sidebar header shows just the KAgent logo.

**Implementation guidance:**

Create `ui/src/components/sidebars/AppSidebarNav.tsx`:
- Define `NAV_SECTIONS` constant (typed array) matching the nav structure from design:
  - OVERVIEW: Dashboard (`/`), Live Feed (`/feed`)
  - AGENTS: My Agents (`/agents`), Workflows (`/workflows`), Cron Jobs (`/cronjobs`), Kanban (`/kanban`)
  - RESOURCES: Models (`/models`), Tools (`/tools`), MCP Servers (`/servers`), GIT Repos (`/git`)
  - ADMIN: Organization (`/admin/org`), Gateways (`/admin/gateways`)
- Use `SidebarGroup`, `SidebarGroupLabel`, `SidebarMenu`, `SidebarMenuItem`, `SidebarMenuButton`
- Detect active route: `const pathname = usePathname()`; pass `isActive={pathname === item.href}` to `SidebarMenuButton`
- Icons from `lucide-react`: `LayoutDashboard`, `Activity`, `Bot`, `GitBranch`, `Clock`, `LayoutGrid`, `Brain`, `Wrench`, `Server`, `GitFork`, `Building2`, `Network`
- Wrap in `"use client"` (needs `usePathname`)

Create `ui/src/components/sidebars/AppSidebar.tsx`:
- `"use client"`
- Renders `<Sidebar collapsible="icon">` with:
  - `<SidebarHeader>`: KAgent logo (`<KagentLogo>` + "KAgent" text)
  - `<SidebarContent>`: `<AppSidebarNav />`
  - `<SidebarFooter>`: placeholder `<div>` (filled in Step 7)
  - `<SidebarRail />`

**Test requirements:**
- Unit test `AppSidebarNav`:
  - Renders all 4 section labels
  - Renders correct number of nav items (12 total)
  - Item matching current `pathname` receives `data-active="true"` (or `aria-current="page"`)
  - Items NOT matching pathname do not have active state
- Use `next/navigation` mock: `jest.mock('next/navigation', () => ({ usePathname: () => '/agents' }))`

**Integration notes:** Component exists but is not yet mounted in any layout. Safe to iterate.

**Demo:** Render `AppSidebar` in isolation (e.g., a test page at `/test-sidebar`) to visually verify structure and active states.

---

## Step 3: Root layout migration

**Objective:** Wire `AppSidebar` into `app/layout.tsx` as the global navigation. Remove `Header` and `Footer`. Every page now shows the left sidebar.

**Implementation guidance:**

Edit `ui/src/app/layout.tsx`:
1. Import `SidebarProvider`, `SidebarInset` from `@/components/ui/sidebar`
2. Import `AppSidebar` from `@/components/sidebars/AppSidebar`
3. Replace `<Header />` and `<main>` wrapper:
   ```tsx
   // Before:
   <Header />
   <main className="flex-1 overflow-y-scroll w-full mx-auto">{children}</main>
   <Footer />

   // After:
   <SidebarProvider>
     <AppSidebar />
     <SidebarInset className="flex-1 overflow-y-auto">
       {children}
     </SidebarInset>
   </SidebarProvider>
   ```
4. Remove `Header` and `Footer` imports
5. `body` className: change `flex flex-col h-screen overflow-hidden` → `flex h-screen overflow-hidden` (SidebarInset handles scroll)
6. Add `<NamespaceProvider>` wrapping (from Step 1) around `SidebarProvider` if not already done

Each page (`/agents`, `/models`, `/tools`, `/servers`, `/`) should continue to render correctly — they become the `{children}` inside `SidebarInset`.

**Test requirements:**
- E2E smoke test: navigate to `/`, `/agents`, `/models`, `/tools`, `/servers` — assert sidebar is present (selector: `nav[aria-label="Main navigation"]`) and page content loads
- Assert `Header` component is NOT in DOM on any page

**Integration notes:**
- Pages that previously relied on `Header` for visual spacing may need top padding added (check each page's outermost div)
- `SidebarTrigger` for mobile not yet added (Step 6) — mobile experience temporarily broken, acceptable at this stage

**Demo:** Open app — every page has the left sidebar. Clicking nav items navigates correctly. Active item highlights on each page.

---

## Step 4: NamespaceSelector in sidebar header

**Objective:** Replace the placeholder logo-only header with a full sidebar header that includes the KAgent logo AND a namespace dropdown. Selecting a namespace updates `NamespaceProvider` context.

**Implementation guidance:**

Create `ui/src/components/sidebars/NamespaceSelector.tsx`:
- `"use client"`
- Thin wrapper around `NamespaceCombobox` logic adapted for sidebar:
  - Compact trigger button (no full-width outline, more subdued styling to fit sidebar)
  - Props: `value: string`, `onValueChange: (ns: string) => void`
  - Internally calls `listNamespaces()`, applies same default selection logic as `NamespaceCombobox`
  - When collapsed (icon mode), show only a K8s namespace icon (e.g. `Network`) with tooltip showing current namespace

Update `AppSidebar.tsx`:
```tsx
const { namespace, setNamespace } = useNamespace();
// In SidebarHeader:
<NamespaceSelector value={namespace} onValueChange={setNamespace} />
```

Update pages that consume namespace (e.g., `app/agents/page.tsx`, `app/models/page.tsx`, etc.) to read namespace from `useNamespace()` instead of local state where applicable. For server components, namespace may need to be passed via URL param or the pages may remain unchanged if they already handle namespace internally.

**Test requirements:**
- Unit test `NamespaceSelector`:
  - Renders namespace name from props
  - Calls `onValueChange` when a namespace is selected from dropdown
  - Shows loading spinner while namespaces are loading
- Integration test: selecting a namespace in sidebar updates `useNamespace()` context value

**Integration notes:** Pages currently manage namespace locally — this step does NOT force pages to read from context (that's a follow-on). The selector just needs to be functional in the sidebar UI.

**Demo:** Sidebar header shows KAgent logo + namespace dropdown. Selecting different namespaces updates the displayed value.

---

## Step 5: Chat layout conflict resolution

**Objective:** Fix the two-left-sidebars conflict in chat routes. Move `SessionsSidebar` to `side="right"`, convert `AgentDetailsSidebar` to a `Sheet` panel, and remove the redundant inner `SidebarProvider` from the chat layout.

**Implementation guidance:**

Edit `ui/src/app/agents/[namespace]/[name]/chat/layout.tsx`:
- Remove `SidebarProvider` wrapper entirely (global one from root layout is sufficient)
- Remove `--sidebar-width` CSS variable override (no longer needed)

Edit `ui/src/components/sidebars/SessionsSidebar.tsx`:
- Change `<Sidebar side="left" ...>` → `<Sidebar side="right" collapsible="offcanvas">`

Edit `ui/src/components/sidebars/AgentDetailsSidebar.tsx`:
- Replace `<Sidebar side="right">` with `<Sheet>`:
  ```tsx
  // Becomes a Sheet triggered externally
  export function AgentDetailsSidebar({ open, onClose, ... }) {
    return (
      <Sheet open={open} onOpenChange={onClose}>
        <SheetContent side="right" className="w-[350px]">
          {/* existing content unchanged */}
        </SheetContent>
      </Sheet>
    );
  }
  ```
- Add `open: boolean` and `onClose: () => void` props

Edit `ui/src/components/chat/ChatLayoutUI.tsx`:
- Add `const [agentDetailsOpen, setAgentDetailsOpen] = useState(false)`
- Add a trigger button (e.g., info icon `<Info>` in chat header) to toggle `agentDetailsOpen`
- Pass `open={agentDetailsOpen}` and `onClose={() => setAgentDetailsOpen(false)}` to `AgentDetailsSidebar`

**Test requirements:**
- Unit test `AgentDetailsSidebar`: renders as `Sheet`, `open` prop controls visibility
- E2E test: navigate to a chat page — assert global left sidebar present, `SessionsSidebar` on right, no layout overlap

**Integration notes:**
- The `SessionsSidebar` on `side="right"` consumes the RIGHT side of the `SidebarProvider` context. shadcn supports both sides — no conflict with the global left `AppSidebar` because they use separate `SidebarProvider` instances (global vs. none now — `SessionsSidebar` just renders as a right-side panel within whatever `SidebarProvider` is active)
- Watch for `SidebarProvider` context confusion: `SessionsSidebar` uses `useSidebar()` internally. With only the global `SidebarProvider`, the toggle for `SessionsSidebar` (offcanvas right) will use the global sidebar context toggle. This means the global `SidebarTrigger` toggles the LEFT sidebar and `SessionsSidebar` needs its own toggle. Add a dedicated `SidebarTrigger` button in the chat header for the sessions panel.

**Demo:** Chat page: global left nav sidebar visible, sessions list on right panel, agent details open in a Sheet overlay from a button click. No layout breakage.

---

## Step 6: Mobile trigger bar

**Objective:** Add a minimal top bar visible only on mobile that contains the `SidebarTrigger` (hamburger button) and the page title/logo. On desktop this bar is hidden.

**Implementation guidance:**

Create `ui/src/components/MobileTopBar.tsx`:
```tsx
"use client";
import { SidebarTrigger } from "@/components/ui/sidebar";
import KAgentLogoWithText from "./kagent-logo-text";

export function MobileTopBar() {
  return (
    <div className="flex items-center gap-2 px-4 py-3 border-b lg:hidden">
      <SidebarTrigger />
      <KAgentLogoWithText className="h-5" />
    </div>
  );
}
```

Edit `app/layout.tsx` — inside `SidebarInset`, prepend `<MobileTopBar />`:
```tsx
<SidebarInset className="flex-1 overflow-y-auto">
  <MobileTopBar />
  {children}
</SidebarInset>
```

The existing `SidebarProvider` in shadcn already handles mobile Sheet behavior via `useIsMobile()` hook — no extra code needed for the sheet overlay itself.

**Test requirements:**
- Unit test `MobileTopBar`: renders `SidebarTrigger` and logo; has `lg:hidden` class
- E2E test (viewport 375px): `MobileTopBar` is visible; clicking hamburger opens sidebar as overlay sheet; clicking a nav item closes the sheet and navigates

**Integration notes:** On desktop (`lg:` breakpoint and above), `MobileTopBar` is hidden via Tailwind. The sidebar is always visible on desktop without a trigger.

**Demo:** Resize to mobile width — top bar with hamburger appears. Tap to open sidebar overlay. Tap nav item — sidebar closes and page changes.

---

## Step 7: StatusIndicator + sidebar footer polish + accessibility

**Objective:** Complete the sidebar with a working footer (status indicator + theme toggle) and ensure all accessibility requirements are met.

**Implementation guidance:**

Create `ui/src/components/sidebars/StatusIndicator.tsx`:
- `"use client"`
- Static display: green dot + "All systems operational" text
- In collapsed mode (icon-only): show only the green dot with a tooltip
- Use `useSidebar()` to detect `state === "collapsed"` for icon-only variant

Update `AppSidebar.tsx` `<SidebarFooter>`:
```tsx
<SidebarFooter>
  <StatusIndicator />
  <ThemeToggle />
</SidebarFooter>
```

Accessibility attributes (edit `AppSidebar.tsx` and `AppSidebarNav.tsx`):
- Add `aria-label="Main navigation"` to the `<Sidebar>` or its inner `<nav>` element
- Each `SidebarGroup` gets `role="group"` and `aria-labelledby={sectionId}` where `sectionId` references the `SidebarGroupLabel`'s `id`
- `SidebarMenuButton` with `isActive` gets `aria-current="page"`
- Verify all items meet WCAG 2.1 AA contrast (check against both light and dark themes)

**Test requirements:**
- Unit test `StatusIndicator`: renders green dot + text in expanded state; renders only dot with tooltip in collapsed state
- Accessibility audit: run `axe-core` (or `@axe-core/react`) assertions on rendered `AppSidebar` — zero critical/serious violations
- Visual check: sidebar footer visible in expanded and collapsed states

**Integration notes:** `ThemeToggle` component already exists — import directly. No new dependencies.

**Demo:** Sidebar footer shows status and theme toggle. Collapsing sidebar to icon mode — only icons with tooltips visible throughout. Tab through sidebar with keyboard — all items focusable, active item has `aria-current="page"`.

---

## Step 8: Placeholder route stub pages

**Objective:** Create minimal stub pages for all "new" routes so nav items don't 404. Each stub renders a "Coming Soon" message with the section name.

**Implementation guidance:**

Create the following files, each with a minimal page component:
- `ui/src/app/feed/page.tsx` → "Live Feed — Coming Soon"
- `ui/src/app/workflows/page.tsx` → "Workflows — Coming Soon"
- `ui/src/app/cronjobs/page.tsx` → "Cron Jobs — Coming Soon"
- `ui/src/app/kanban/page.tsx` → "Kanban — Coming Soon"
- `ui/src/app/git/page.tsx` → "GIT Repos — Coming Soon"
- `ui/src/app/admin/org/page.tsx` → "Organization — Coming Soon"
- `ui/src/app/admin/gateways/page.tsx` → "Gateways — Coming Soon"

Each stub page:
```tsx
export default function FeedPage() {
  return (
    <div className="flex flex-col items-center justify-center h-full min-h-[400px] gap-4 text-muted-foreground">
      <Activity className="h-12 w-12 opacity-30" />
      <p className="text-lg font-medium">Live Feed</p>
      <p className="text-sm">Coming soon</p>
    </div>
  );
}
```

**Test requirements:**
- E2E test: click each nav item — assert no 404, page title/heading matches nav label, sidebar active state matches

**Integration notes:** These are pure server components (no `"use client"` needed). No data fetching.

**Demo:** Click every nav item in the sidebar — all navigate without error. Active item highlights correctly on each stub page.
