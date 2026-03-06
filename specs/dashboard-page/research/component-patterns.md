# UI Component Patterns Research

## Available Shadcn/UI Components
Key components in `ui/src/components/ui/`:
- **Card** (Card, CardHeader, CardTitle, CardDescription, CardContent, CardFooter)
- **Badge** (variants: default, secondary, destructive, outline)
- **Table** (full HTML table with Radix primitives)
- **Tabs** (tab navigation)
- **Progress** (progress bar)
- **Alert** (alert boxes)
- **Dialog/AlertDialog** (modals)
- **Tooltip** (overlays)
- **ScrollArea** (custom scrollbar)
- **Collapsible** (expand/collapse)
- **Separator** (divider)

## Chart Library
**None installed.** No recharts, chart.js, visx, or similar in package.json.

However, **5 chart colors are pre-defined** in CSS variables:
- `--chart-1` through `--chart-5` (with dark mode variants)

**Recommendation:** Install `recharts` (most common with Shadcn/UI) for the Agent Activity chart.

## Existing Page Patterns

### Agent Card (`AgentCard.tsx`)
- Card with hover: `group relative transition-all duration-200`
- Status badges: red-500/10, yellow-400/30
- Dropdown menu for actions
- Responsive grid: `grid-cols-1 md:grid-cols-2 lg:grid-cols-3 gap-6`

### Models List
- Expandable rows with inline edit/delete
- Layout: `min-h-screen p-8` > `max-w-6xl mx-auto`

### Tools Page
- Category grouping with collapsible sections
- Search + filter UI
- ScrollArea with `h-[calc(100vh-300px)]`

## Theme & Styling

### Color System (CSS variables)
- `--background/--foreground` — base colors
- `--card/--card-foreground` — card surfaces
- `--primary` — purple-ish accent
- `--destructive` — red for errors
- `--muted` — subdued text
- Dark mode: `darkMode: "class"` with `.dark` selector

### Sidebar Colors
- `--sidebar-background/foreground/primary/accent/border`

### Common Layout Classes
- Page: `min-h-screen p-8`
- Container: `max-w-6xl mx-auto`
- Spacing: `space-y-4`
- Responsive grid: `grid grid-cols-1 md:grid-cols-2 lg:grid-cols-3`

## Icons
**Lucide React** (v0.562.0) — used throughout. Standard sizes: `h-4 w-4`, `h-5 w-5`.

## What Needs to Be Built
1. **Stat card component** — Card + icon + count + label (no existing component)
2. **Activity chart** — needs recharts or similar
3. **Recent runs list** — can adapt from existing session/task patterns
4. **Live feed mini-panel** — embed of /feed functionality
