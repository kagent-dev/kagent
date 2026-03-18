# Kagent UI Development Guide

Next.js web interface: components, routing, state management, and testing.

**See also:** [architecture.md](architecture.md) (system overview), [testing-ci.md](testing-ci.md) (test commands)

---

## Local development

### Prerequisites

- Node.js 24.13.0+ (see `ui/.nvmrc`)
- npm

### Essential commands

| Task | Command |
|------|---------|
| Install dependencies | `npm ci` (in `ui/`) |
| Build | `make -C ui build` |
| Clean | `make -C ui clean` |
| Security audit | `make -C ui audit` |
| Update deps | `make -C ui update` |
| Access UI (dev) | `kubectl port-forward -n kagent svc/kagent-ui 3000:8080` |

### CI commands

CI runs linting and Jest tests:

```bash
cd ui && npm ci && npx next lint && npx jest
```

## Project structure

```
ui/
├── src/
│   ├── app/                    # Next.js app router
│   │   ├── agents/             # Agent management pages
│   │   │   ├── new/            # Create agent wizard
│   │   │   └── [namespace]/[name]/  # Agent detail + chat
│   │   ├── models/             # Model configuration pages
│   │   ├── servers/            # MCP server management
│   │   ├── tools/              # Tool management
│   │   ├── a2a/                # Agent-to-Agent pages
│   │   └── actions/            # Server actions
│   ├── components/             # Reusable React components
│   │   ├── chat/               # Chat interface (hub)
│   │   ├── create/             # Creation wizards
│   │   ├── models/             # Model components
│   │   ├── tools/              # Tool components
│   │   ├── sidebars/           # Navigation
│   │   ├── ui/                 # Shadcn/ui primitives (30+)
│   │   └── icons/              # Icon components
│   ├── hooks/                  # Custom React hooks
│   ├── types/                  # TypeScript type definitions
│   └── lib/                    # Utility functions
│       ├── a2aClient.ts        # A2A JSON-RPC client
│       ├── messageHandlers.ts  # A2A message parsing (hub file)
│       ├── toolUtils.ts        # Tool utilities
│       ├── k8sUtils.ts         # Kubernetes utilities
│       └── providers.ts        # LLM provider info
├── cypress/                    # E2E test framework
├── package.json
├── tsconfig.json
└── next.config.js
```

## Technologies

| Technology | Purpose |
|------------|---------|
| Next.js 16 | App router, server actions |
| React 19 | UI framework |
| Radix UI | Accessible component primitives |
| Shadcn/ui | Styled component library |
| TailwindCSS | Utility-first styling |
| Zustand | State management |
| React Hook Form + Zod | Form handling + validation |
| Lucide React | Icons |
| react-markdown | Markdown rendering |
| @a2a-js/sdk | A2A protocol client |

## TypeScript standards

- **Strict mode** enabled in `tsconfig.json`
- No `any` type — use proper typing or `unknown` with type guards
- No inline styles — use TailwindCSS classes
- No direct DOM manipulation — use React patterns
- Components should be functional with hooks
- Use descriptive prop interfaces

## Key hub components

These components have the most connections. Changes require extra care:

| Component | Size | Purpose |
|-----------|------|---------|
| `ChatInterface.tsx` | 27KB | Main chat UI — renders messages, handles input, manages streaming |
| `messageHandlers.ts` | 26KB | Parses A2A events, manages message state |
| `ToolCallDisplay.tsx` | 12KB | Renders tool call cards with approval/rejection |
| `ToolDisplay.tsx` | 9.5KB | Tool rendering and management |
| `AgentsProvider.tsx` | 9.4KB | Agent state provider (context) |

## Development patterns

### Adding a new page

1. Create route in `ui/src/app/<route>/page.tsx`
2. Add navigation link in sidebar component
3. Create any needed server actions in `ui/src/app/actions/`
4. Add types in `ui/src/types/`
5. Use existing Shadcn/ui components from `ui/src/components/ui/`

### Adding a new component

1. Create component in appropriate `ui/src/components/<category>/`
2. Use Radix UI primitives for accessibility
3. Style with TailwindCSS
4. Add TypeScript prop interfaces
5. Add Jest tests for logic-heavy components

### A2A client usage

```typescript
import { A2AClient } from '@/lib/a2aClient';

// Send message and handle streaming response
const client = new A2AClient(baseUrl);
await client.sendMessage(agentId, message, {
  onEvent: (event) => {
    // Handle SSE events
  },
});
```
