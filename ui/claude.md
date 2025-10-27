# Kagent Web UI

## Overview

The Kagent Web UI is a Next.js 15 application providing a modern web interface for managing AI agents, model configurations, MCP servers, and interactive chat sessions. Built with React 18, TypeScript, and TailwindCSS, it offers a responsive, accessible interface for the entire Kagent ecosystem.

## Architecture

The UI follows Next.js App Router conventions with:
- **Server Components**: For data fetching and static rendering
- **Client Components**: For interactive UI elements
- **Server Actions**: For API mutations and data operations
- **A2A Integration**: Real-time agent communication via @a2a-js/sdk
- **State Management**: Zustand for global state
- **Styling**: TailwindCSS with Radix UI components

## Technology Stack

### Framework & Core
- **Next.js 15.4.7**: React framework with App Router
- **React 18.3.1**: UI library
- **TypeScript 5.8.3**: Type safety
- **Turbopack**: Fast development bundler (line 6 in package.json)

### UI Components & Styling
- **Radix UI**: Accessible component primitives
  - Dialog, Dropdown, Select, Tooltip, Accordion, etc.
- **TailwindCSS 3.4.17**: Utility-first CSS
- **Lucide React**: Icon library
- **next-themes**: Dark/light mode support
- **class-variance-authority**: Component variant management
- **sonner**: Toast notifications

### Data & Forms
- **React Hook Form 7.62**: Form management
- **Zod 3.25.76**: Schema validation
- **@hookform/resolvers**: Form validation integration

### A2A Integration
- **@a2a-js/sdk 0.2.5**: Agent-to-Agent protocol client
- **uuid**: Session ID generation

### Content Rendering
- **react-markdown 9.1**: Markdown rendering
- **remark-gfm**: GitHub Flavored Markdown
- **rehype-external-links**: Link handling
- **@tailwindcss/typography**: Prose styling

### Testing
- **Jest 29.7**: Unit testing
- **Cypress 14.5**: E2E testing
- **@testing-library/react**: Component testing
- **msw 2.10.2**: API mocking

## Directory Structure

```
ui/
├── src/
│   ├── app/                     # Next.js App Router
│   │   ├── actions/             # Server Actions (API calls)
│   │   │   ├── agents.ts        # Agent CRUD operations
│   │   │   ├── sessions.ts      # Session management
│   │   │   ├── modelConfigs.ts  # Model config operations
│   │   │   ├── servers.ts       # MCP server operations
│   │   │   ├── tools.ts         # Tool management
│   │   │   └── feedback.ts      # User feedback
│   │   ├── agents/              # Agent pages
│   │   │   ├── page.tsx         # Agent list view
│   │   │   ├── new/             # Create agent
│   │   │   └── [namespace]/[name]/chat/  # Chat interface
│   │   ├── models/              # Model config pages
│   │   │   ├── page.tsx         # Model list
│   │   │   └── new/             # Create model config
│   │   ├── servers/             # MCP server pages
│   │   ├── tools/               # Tool browser
│   │   ├── a2a/                 # A2A API routes
│   │   ├── layout.tsx           # Root layout (lines 1-41)
│   │   └── page.tsx             # Home page
│   ├── components/              # React components
│   │   ├── chat/                # Chat interface components
│   │   │   ├── ChatInterface.tsx    # Main chat (lines 1-100+)
│   │   │   ├── ChatMessage.tsx      # Message display
│   │   │   ├── StreamingMessage.tsx # Real-time updates
│   │   │   ├── StatusDisplay.tsx    # Task status
│   │   │   ├── TokenStats.tsx       # Token usage
│   │   │   ├── ToolCallDisplay.tsx  # Tool execution
│   │   │   └── CodeBlock.tsx        # Code rendering
│   │   ├── sidebars/            # Navigation sidebars
│   │   │   ├── SessionsSidebar.tsx  # Session history
│   │   │   ├── AgentDetailsSidebar.tsx # Agent info
│   │   │   └── AgentSwitcher.tsx    # Agent selection
│   │   ├── create/              # Agent creation flow
│   │   ├── models/              # Model config components
│   │   ├── onboarding/          # Onboarding wizard
│   │   ├── ui/                  # Radix UI wrappers
│   │   ├── icons/               # Provider logos
│   │   └── [various].tsx        # Shared components
│   ├── lib/                     # Utilities
│   │   ├── a2aClient.ts         # A2A SDK client
│   │   ├── messageHandlers.ts   # Message processing
│   │   └── statusUtils.ts       # Status formatting
│   ├── hooks/                   # Custom React hooks
│   ├── types/                   # TypeScript types
│   └── store/                   # Zustand stores
├── cypress/                     # E2E tests
├── public/                      # Static assets
├── package.json                 # Dependencies (lines 1-82)
└── README.md                    # Documentation
```

## Key Features

### 1. Agent Management

#### Agent List (`app/agents/page.tsx`)
- **Display**: Grid/list view of all agents
- **Filtering**: By namespace, status
- **Actions**: Create, view, delete agents
- **Components**: `AgentGrid`, `AgentCard`, `AgentList`

#### Create Agent (`app/agents/new/page.tsx`)
- **Wizard Flow**:
  1. Basic info (name, description)
  2. Model selection (provider, model name)
  3. Tools selection (MCP servers, tools)
  4. System prompt configuration
  5. Review and deploy
- **Validation**: Zod schemas with react-hook-form
- **Components**: `ModelSelectionSection`, `ToolsSection`, `SystemPromptSection`

#### Agent Details Sidebar (`components/sidebars/AgentDetailsSidebar.tsx`)
- **Information**: Agent metadata, status, configuration
- **Actions**: Edit, delete, view deployment
- **Tools**: List of configured tools and MCP servers

### 2. Interactive Chat

#### Chat Interface (`components/chat/ChatInterface.tsx`)
- **Features** (lines 31-100+):
  - Real-time message streaming via A2A protocol
  - Session persistence and history
  - Token usage tracking
  - Status indicators (working, streaming, error)
  - Message abort functionality
  - Auto-scroll to latest messages
- **Message Types**:
  - User messages
  - Assistant responses
  - Tool calls and results
  - Agent-to-agent calls
  - Status updates
- **State Management**:
  - Stored messages (history)
  - Streaming messages (current)
  - Streaming content (partial response)
  - Chat status (ready, working, streaming)

#### Chat Message Display (`components/chat/ChatMessage.tsx`)
- **Rendering**:
  - Markdown support with syntax highlighting
  - Code blocks with copy button
  - Tool call visualization
  - Agent call display
  - Artifact rendering
- **Feedback**: Thumbs up/down with comments

#### Streaming Message (`components/chat/StreamingMessage.tsx`)
- **Purpose**: Render partial agent responses
- **Features**: Typing indicator, smooth updates

#### Tool Call Display (`components/chat/ToolCallDisplay.tsx`)
- **Visualization**: Tool name, parameters, results
- **Collapsible**: Expand/collapse for details

#### Status Display (`components/chat/StatusDisplay.tsx`)
- **States**: Working, streaming, error, completed
- **Indicators**: Spinner, progress, status text

#### Token Stats (`components/chat/TokenStats.tsx`)
- **Metrics**: Input, output, total tokens
- **Cost**: Estimated usage cost (if available)

### 3. Session Management

#### Sessions Sidebar (`components/sidebars/SessionsSidebar.tsx`)
- **Features**:
  - Session history for current agent
  - Grouped by date (Today, Yesterday, Last 7 days, etc.)
  - Search and filter
  - Create new session
  - Delete session
- **Components**: `SessionGroup`, `ChatItem`, `GroupedChats`

#### Session Actions (`app/actions/sessions.ts`)
- **Operations**:
  - `createSession()`: Create new chat session
  - `getSessionTasks()`: Load message history
  - `checkSessionExists()`: Validate session
  - `deleteSession()`: Remove session

### 4. Model Configuration

#### Model Config List (`app/models/page.tsx`)
- **Display**: Available model configurations
- **Providers**: OpenAI, Anthropic, Azure, Google, Ollama
- **Actions**: Create, edit, delete configs

#### Create Model Config (`app/models/new/page.tsx`)
- **Sections**:
  - Basic info (name, description)
  - Provider selection
  - Authentication (API keys, endpoints)
  - Parameters (temperature, max tokens, etc.)
- **Components**:
  - `BasicInfoSection`
  - `AuthSection`
  - `ParamsSection`
  - `ModelProviderCombobox`

### 5. MCP Server Management

#### Server List (`app/servers/page.tsx`)
- **Display**: Configured MCP servers
- **Actions**: Add, configure, remove servers
- **Component**: `AddServerDialog`

#### Tool Browser (`app/tools/page.tsx`)
- **Features**:
  - Browse all available tools
  - Category filtering
  - Search functionality
  - Tool details and schemas
- **Components**: `ToolDisplay`, `CategoryFilter`

### 6. Onboarding

#### Onboarding Wizard (`components/onboarding/OnboardingWizard.tsx`)
- **Steps**:
  1. Welcome
  2. Model config setup
  3. Agent setup
  4. Tool selection
  5. Review
  6. Finish
- **Purpose**: Guide new users through initial setup

### 7. Theme & Styling

#### Theme Provider (`components/ThemeProvider.tsx`)
- **Modes**: Light, dark, system
- **Persistence**: LocalStorage
- **Toggle**: `ThemeToggle` component

#### Header (`components/Header.tsx`)
- **Navigation**: Links to agents, models, tools, servers
- **Actions**: Settings, theme toggle
- **Branding**: Kagent logo

#### Footer (`components/Footer.tsx`)
- **Links**: Documentation, GitHub, Twitter
- **Version**: Display app version

### 8. A2A Integration

#### A2A Client (`lib/a2aClient.ts`)
- **Purpose**: Singleton A2A SDK client
- **Configuration**: Base URL, headers
- **Features**: Message streaming, task management

#### Message Handlers (`lib/messageHandlers.ts`)
- **Functions**:
  - `createMessageHandlers()`: Event handler factory
  - `extractMessagesFromTasks()`: Parse task history
  - `extractTokenStatsFromTasks()`: Calculate token usage
  - `createMessage()`: Message object builder

#### A2A API Route (`app/a2a/[namespace]/[agentName]/route.ts`)
- **Purpose**: Proxy A2A requests to backend
- **Method**: POST for message streaming
- **Headers**: Authentication, user ID

### 9. Server Actions

All API calls are implemented as Next.js Server Actions in `app/actions/`:

#### Agent Actions (`actions/agents.ts`)
- `getAgents()`: List all agents
- `getAgent()`: Get agent details
- `createAgent()`: Deploy new agent
- `deleteAgent()`: Remove agent
- `getAgentTools()`: List agent tools

#### Session Actions (`actions/sessions.ts`)
- `createSession()`: Start chat session
- `getSessionTasks()`: Load conversation history
- `checkSessionExists()`: Validate session ID
- `deleteSession()`: Remove session

#### Model Config Actions (`actions/modelConfigs.ts`)
- `getModelConfigs()`: List configs
- `createModelConfig()`: Create config
- `deleteModelConfig()`: Remove config

#### Server Actions (`actions/servers.ts`)
- `getServers()`: List MCP servers
- `addServer()`: Register server
- `removeServer()`: Unregister server

#### Tool Actions (`actions/tools.ts`)
- `getTools()`: Browse available tools
- `getToolsByCategory()`: Filter by category

#### Provider Actions (`actions/providers.ts`)
- `getProviders()`: List LLM providers
- `getModels()`: List models for provider

### 10. State Management

#### Agents Provider (`components/AgentsProvider.tsx`)
- **Purpose**: Global agent state
- **Context**: Current agents, selected agent
- **Features**: Auto-refresh, caching

#### App Initializer (`components/AppInitializer.tsx`)
- **Purpose**: Bootstrap application state
- **Actions**: Load initial data, check auth

## Development

### Running Locally

```bash
# Install dependencies
npm install

# Start development server (line 6 in package.json)
npm run dev

# Runs on http://localhost:8001 with Turbopack
```

### Testing Against Kubernetes

```bash
# Port-forward Kagent controller
kubectl port-forward svc/kagent-controller 8083

# Run UI (will connect to http://localhost:8083)
npm run dev
```

### Building for Production

```bash
# Build optimized bundle
npm run build

# Start production server
npm start
```

### Testing

```bash
# Unit tests
npm test

# Watch mode
npm run test:watch

# E2E tests
npm run test:e2e
```

## Configuration

### Environment Variables
- `NEXT_PUBLIC_API_URL`: Kagent API endpoint (default: http://localhost:8083)
- `NEXT_PUBLIC_USER_ID`: User identifier for sessions

### API Endpoints
All server actions call Kagent REST API:
- `/api/agents`: Agent CRUD
- `/api/sessions`: Session management
- `/api/tasks`: Task history
- `/api/modelconfigs`: Model configurations
- `/api/remote-mcp-servers`: MCP server registry
- `/api/tools`: Tool discovery

## Component Library

### UI Components (`components/ui/`)
Radix UI primitives wrapped with Tailwind styling:
- `button.tsx`: Button variants
- `input.tsx`: Text input
- `textarea.tsx`: Multi-line input
- `select.tsx`: Dropdown select
- `dialog.tsx`: Modal dialogs
- `dropdown-menu.tsx`: Context menus
- `tooltip.tsx`: Hover tooltips
- `badge.tsx`: Status badges
- `card.tsx`: Content cards
- `form.tsx`: Form components
- `alert.tsx`: Alert banners
- `progress.tsx`: Progress bars
- `scroll-area.tsx`: Scrollable containers
- `tabs.tsx`: Tab navigation
- `command.tsx`: Command palette
- `collapsible.tsx`: Expandable sections

### Icon Components (`components/icons/`)
Custom SVG icons for:
- Provider logos (OpenAI, Anthropic, Azure, Gemini, Ollama)
- Social icons (Twitter)
- MCP icon
- Kagent logo and text

## Key Pages

### Home (`app/page.tsx`)
- **Purpose**: Landing page
- **Features**: Quick start guide, recent agents

### Agents (`app/agents/page.tsx`)
- **Purpose**: Browse and manage agents
- **Layout**: Grid or list view

### Agent Chat (`app/agents/[namespace]/[name]/chat/[chatId]/page.tsx`)
- **Purpose**: Interactive chat with agent
- **Features**: Message streaming, history, sessions

### Models (`app/models/page.tsx`)
- **Purpose**: Manage model configurations
- **Features**: CRUD operations for LLM configs

### Servers (`app/servers/page.tsx`)
- **Purpose**: Manage MCP servers
- **Features**: Register, configure, remove servers

### Tools (`app/tools/page.tsx`)
- **Purpose**: Browse available tools
- **Features**: Search, filter, view schemas

## Styling

### TailwindCSS Configuration
- **Typography**: `@tailwindcss/typography` for prose
- **Animations**: `tailwindcss-animate` for transitions
- **Theme**: Custom colors, fonts, spacing
- **Dark Mode**: `class` strategy for theme switching

### Design System
- **Colors**: Primary, secondary, muted, accent, destructive
- **Typography**: Geist font (line 12 in layout.tsx)
- **Spacing**: Consistent padding and margins
- **Borders**: Rounded corners, subtle borders
- **Shadows**: Subtle elevation

## Accessibility

- **Radix UI**: WAI-ARIA compliant components
- **Keyboard Navigation**: Full keyboard support
- **Screen Readers**: Semantic HTML and ARIA labels
- **Focus Management**: Visible focus indicators
- **Color Contrast**: WCAG AA compliant

## Performance

- **Turbopack**: Fast development builds
- **Server Components**: Reduced client JavaScript
- **Code Splitting**: Automatic route-based splitting
- **Image Optimization**: Next.js image component
- **Font Optimization**: Variable font loading

## Related Components

- **Controller** (`go/internal/controller/`): Backend Kubernetes controller
- **HTTP Server** (`go/internal/httpserver/`): REST API
- **CLI** (`go/cli/`): Command-line interface
- **Python Packages** (`python/packages/`): Agent runtimes

## Future Enhancements

- **Real-time Collaboration**: Multi-user chat sessions
- **Agent Marketplace**: Discover community agents
- **Advanced Analytics**: Usage metrics, performance graphs
- **Custom Themes**: User-defined color schemes
- **Inline Agent Editing**: Modify agent config in UI
- **Tool Testing**: Interactive tool execution sandbox
- **Deployment Pipeline**: Visual deployment workflows
- **Monitoring Dashboard**: Real-time system health
