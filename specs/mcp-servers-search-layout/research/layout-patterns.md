# Layout Patterns Across Pages

## Container Classes

| Page | Outer Container | max-w | ScrollArea | Search |
|------|----------------|-------|------------|--------|
| servers | `mt-12 mx-auto max-w-6xl px-6` | 6xl | No | No |
| tools | `mt-12 mx-auto max-w-6xl px-6 pb-12` | 6xl | Yes | Yes + category filter |
| models | `min-h-screen p-8` + `max-w-6xl mx-auto` | 6xl | No | No |
| cronjobs | `min-h-screen p-8` + `max-w-6xl mx-auto` | 6xl | No | No |
| git | `min-h-screen p-8` + `max-w-6xl mx-auto` | 6xl | No | Yes (backend search) |
| plugins | `mt-12 mx-auto max-w-4xl px-6` | 4xl | No | No |
| agents | Delegates to AgentList | N/A | N/A | N/A |
| feed | Placeholder | N/A | N/A | N/A |

## Key Findings

- Both Tools and Servers pages already use the **same** `max-w-6xl` constraint
- "Stretch layout" likely refers to the Tools page's `ScrollArea` with `h-[calc(100vh-300px)]` which fills available viewport height
- Tools page also has `pb-12` bottom padding that Servers lacks
- No shared page layout wrapper exists — each page defines its own container
