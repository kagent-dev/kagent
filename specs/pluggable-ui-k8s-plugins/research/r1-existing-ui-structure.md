# Research: Existing UI Structure

## Current Layout (`app/layout.tsx`)

```
<TooltipProvider>
  <AgentsProvider>
    <html>
      <body class="flex flex-col h-screen overflow-hidden">
        <ThemeProvider>
          <AppInitializer>
            <Header />          ← top nav bar
            <main>              ← full-width scroll area
              {children}
            </main>
            <Footer />          ← simple logo footer
          </AppInitializer>
        </ThemeProvider>
      </body>
    </html>
  </AgentsProvider>
</TooltipProvider>
```

**No global `SidebarProvider` at root level.** The only `SidebarProvider` usage is inside the chat-specific nested layout.

---

## Existing Sidebar Primitives (`components/ui/sidebar.tsx`)

Full shadcn/ui sidebar primitives are present:
- `SidebarProvider` — context + cookie persistence (`sidebar_state`)
- `Sidebar` — supports `side`, `collapsible="offcanvas"|"icon"|"none"`
- `SidebarContent`, `SidebarHeader`, `SidebarFooter`
- `SidebarMenu`, `SidebarMenuItem`, `SidebarMenuButton`
- `SidebarRail` — drag-resize rail
- `SidebarInset` — main content wrapper that adjusts to sidebar state
- `SidebarTrigger` — hamburger toggle button
- Constants: `SIDEBAR_COOKIE_NAME = "sidebar_state"`, widths: 16rem expanded, 3rem icon-only

---

## Chat Layout (`app/agents/[namespace]/[name]/chat/layout.tsx`)

Wraps in its own `SidebarProvider` (width overridden to 350px):
```tsx
<SidebarProvider style={{ "--sidebar-width": "350px" }}>
  <ChatLayoutUI>   ← renders SessionsSidebar (left) + AgentDetailsSidebar (right)
    {children}
  </ChatLayoutUI>
</SidebarProvider>
```

`SessionsSidebar` uses `<Sidebar side="left" collapsible="offcanvas">` — **will conflict** with a global left AppSidebar.

---

## Header.tsx Routes (current)

| Route | Label |
|-------|-------|
| `/` | Home |
| `/agents` | My Agents (View dropdown) |
| `/agents/new` | New Agent (Create dropdown) |
| `/models` | Models |
| `/models/new` | New Model |
| `/tools` | Tools (labeled "MCP Tools") |
| `/servers` | MCP Servers |

---

## NamespaceCombobox Component

`components/NamespaceCombobox.tsx` — full-featured: loads from `listNamespaces()`, auto-selects "kagent" > "default" > first, Popover+Command pattern. **Can be adapted/reused** in `NamespaceSelector.tsx`.

---

## AgentsProvider Context

Global context providing: `agents`, `models`, `tools`, `loading`, `error` + refresh functions. **Does NOT manage namespace.** Namespace is handled per-page locally (e.g., in agents page, chat page).

---

## Footer.tsx

Simple: KAgent animated logo + "is an open source project" text. Can be removed and its content moved to the sidebar footer.

---

## Key Conflicts & Constraints

1. **Nested SidebarProvider**: shadcn `SidebarProvider` uses React Context. If root layout also has one, the chat layout's inner `SidebarProvider` overrides it for chat routes (React context nearest-ancestor wins). This means the global sidebar state and the chat sidebar state would be decoupled — **acceptable**.

2. **SessionsSidebar side="left" conflict**: With a global AppSidebar on the left, `SessionsSidebar` occupying the same side would render two left sidebars. Must change `SessionsSidebar` to use `side="right"` OR render it as a panel inside `SidebarInset`, OR remove `SidebarProvider` from chat layout and integrate sessions into a secondary sidebar using a different primitive (e.g., Sheet or Collapsible).

3. **Namespace global state**: No global namespace context exists. Need to add a `NamespaceProvider` (or extend `AgentsProvider`) to propagate the selected namespace app-wide.

4. **`SidebarInset` wrapper**: Root layout must replace `<main>` with `<SidebarInset>` for content to auto-adjust to sidebar width.
