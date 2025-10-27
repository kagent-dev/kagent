## Tech stack

This document defines the technical stack for the Kagent project. The Kagent framework is a Kubernetes-native platform for building and deploying AI agents.

### Project Architecture

**Multi-Language Monorepo:**
- **Go**: Controller, API server, CLI
- **Python**: Agent runtimes (ADK, LangGraph, CrewAI)
- **TypeScript/Next.js**: Web UI

### Framework & Runtime

#### Controller & API (Go)
- **Language**: Go 1.25.1
- **HTTP Router**: Gorilla Mux (`github.com/gorilla/mux`)
- **ORM**: GORM (`gorm.io/gorm`)
- **Database Drivers**:
  - `gorm.io/driver/postgres` - Production database
  - `github.com/glebarez/sqlite` - Development/testing
- **CLI Framework**: Cobra (`github.com/spf13/cobra`)
- **Configuration**: Viper (`github.com/spf13/viper`)
- **Kubernetes Client**: `k8s.io/client-go`, `k8s.io/apimachinery`

#### Python Agent Runtimes
- **Runtime**: Python 3.11.0+
- **Package Manager**: uv (via `hatchling` build backend)
- **Agent Frameworks**:
  - Google ADK (`google-adk`)
  - LangGraph (`langgraph`)
  - CrewAI (`crewai`)
- **A2A Protocol**: `a2a-sdk` - Agent-to-Agent communication
- **STS Integration**: `agentsts-adk`, `agentsts-core` - Stateless Transaction Service
- **HTTP Client**: `httpx` - Async HTTP for Python
- **Validation**: `pydantic` - Type-safe data validation
- **LLM Providers**:
  - `openai` - OpenAI API
  - `anthropic[vertex]` - Anthropic/Claude with Vertex AI
  - `google-genai` - Google Gemini
  - `litellm` - Multi-provider LLM gateway

### Frontend

#### Framework
- **Next.js**: 15.4.7 (App Router)
- **React**: 18.3.1
- **TypeScript**: 5.8.3
- **Build Tool**: Turbopack (Next.js built-in)

#### UI Components & Styling
- **Component Library**: Radix UI primitives
  - `@radix-ui/react-dialog` - Modals
  - `@radix-ui/react-dropdown-menu` - Dropdowns
  - `@radix-ui/react-select` - Select inputs
  - `@radix-ui/react-tooltip` - Tooltips
  - `@radix-ui/react-tabs` - Tabs
  - And 15+ other primitives
- **CSS Framework**: Tailwind CSS 3.4.17
- **Styling Utilities**:
  - `class-variance-authority` - Type-safe variants
  - `tailwind-merge` - Class conflict resolution
  - `clsx` - Conditional classes
- **Animations**: `tailwindcss-animate`
- **Typography**: `@tailwindcss/typography` - Prose styling
- **Icons**: `lucide-react` - Icon library
- **Theme**: `next-themes` - Dark/light mode

#### Data & Forms
- **Form Management**: `react-hook-form` 7.62
- **Validation**: `zod` 3.25.76
- **Form Integration**: `@hookform/resolvers`
- **State Management**: `zustand` 5.0.7 - Global state
- **A2A Client**: `@a2a-js/sdk` 0.2.5 - Agent communication

#### Content Rendering
- **Markdown**: `react-markdown` 9.1
- **Markdown Extensions**:
  - `remark-gfm` - GitHub Flavored Markdown
  - `rehype-external-links` - Link handling
- **Utilities**:
  - `date-fns` - Date formatting
  - `uuid` - ID generation

### Database & Storage

#### Primary Database
- **Production**: PostgreSQL
  - GORM driver: `gorm.io/driver/postgres`
  - Full-featured RDBMS
  - JSON field support
  - Advanced indexing

#### Development Database
- **Local/Testing**: SQLite
  - GORM driver: `github.com/glebarez/sqlite`
  - File-based
  - Zero configuration
  - Compatible schema with PostgreSQL

#### ORM
- **GORM**: `gorm.io/gorm`
  - Auto-migration support
  - JSON data type via `gorm.io/datatypes`
  - Generic CRUD functions
  - Transaction support

#### Caching
- Not currently implemented (future consideration: Redis)

### Testing & Quality

#### Go Testing
- **Test Framework**: Go standard `testing` package
- **Assertions**: `github.com/stretchr/testify`
- **Linting**: `golangci-lint` (configured in `.golangci.yaml`)
- **E2E**: Custom test agents in `go/test/e2e/`

#### Python Testing
- **Test Framework**: `pytest` 8.3.5+
- **Async Testing**: `pytest-asyncio` 0.25.3+
- **Mocking**: `unittest.mock.Mock`
- **Linting/Formatting**: `ruff`
- **Configuration**: `pyproject.toml` with test paths

#### Frontend Testing
- **Unit Tests**: Jest 29.7
- **Component Testing**: `@testing-library/react` 14.3.1
- **User Interaction**: `@testing-library/user-event` 14.6.1
- **DOM Assertions**: `@testing-library/jest-dom` 6.6.3
- **E2E Tests**: Cypress 14.5.0
- **API Mocking**: MSW 2.10.2
- **Test Environment**: `jest-environment-jsdom`
- **Linting**: ESLint 9 with `eslint-config-next`

### Deployment & Infrastructure

#### Orchestration
- **Platform**: Kubernetes
- **Custom Resources**: Agent, ModelConfig, ToolServer CRDs
- **Controller**: Kubernetes controller pattern
- **Operator**: Custom resource management

#### Observability
- **Metrics**: Prometheus (`github.com/prometheus/client_golang`)
- **Tracing**: OpenTelemetry
  - Python: `opentelemetry-api`, `opentelemetry-sdk`
  - Go: OpenTelemetry Go SDK
- **Logging**: Structured logging
  - Go: `go-logr/logr`
  - Python: `logging` module

#### CI/CD
- **CI**: GitHub Actions (`.github/workflows/ci.yaml`)
- **Container Registry**: Docker images
- **Automation**: Make-based build system (`go/Makefile`)

### Third-Party Services

#### LLM Providers
- **OpenAI**: GPT-4, GPT-3.5 via `openai` SDK
- **Anthropic**: Claude models via `anthropic` SDK
- **Google**: Vertex AI and Gemini via `google-genai`, `google-adk`
- **Azure**: OpenAI models via Azure endpoints
- **Ollama**: Local LLM deployment
- **Custom**: Any OpenAI-compatible API via `litellm`

#### MCP (Model Context Protocol)
- **Go MCP Client**: `github.com/mark3labs/mcp-go`
- **Python MCP**: `mcp` SDK
- **Tool Servers**: Kubernetes, Istio, Helm, Argo, Prometheus, Grafana, Cilium

#### Authentication
- Not currently implemented (uses `X-User-ID` header for user context)
- Future: OAuth2, OIDC, or Kubernetes ServiceAccount tokens

#### Email
- Not currently implemented

#### Monitoring
- **Prometheus**: Metrics collection
- **OpenTelemetry**: Distributed tracing
- **Future**: Grafana dashboards

### Development Tools

#### Go Development
- **Build Tool**: Make
- **Dependency Management**: Go modules (`go.mod`)
- **CLI Tools**:
  - `github.com/abiosoft/ishell/v2` - Interactive shell
  - `github.com/briandowns/spinner` - Terminal spinners
  - `github.com/charmbracelet/bubbletea` - Terminal UI
  - `github.com/jedib0t/go-pretty/v6` - Table formatting

#### Python Development
- **Package Manager**: uv
- **Build System**: Hatchling
- **Virtual Environments**: uv-managed
- **Type Checking**: mypy (optional)
- **Formatting**: ruff format
- **Linting**: ruff check

#### Frontend Development
- **Dev Server**: Next.js dev with Turbopack
- **Package Manager**: npm
- **TypeScript**: 5.8.3
- **PostCSS**: 8.5.6
- **Code Formatting**: Prettier (via ESLint)

#### Version Control
- **Git**: Standard git workflow
- **Branching**: Feature branches with PR workflow
- **Commit Conventions**: Conventional commits recommended

### Package Management

#### Go
- **Manager**: Go modules
- **File**: `go.mod`, `go.sum`
- **Commands**: `go get`, `go mod tidy`

#### Python
- **Manager**: uv
- **Config**: `pyproject.toml`
- **Workspaces**: Monorepo with workspace configuration
- **Build**: Hatchling

#### Node.js
- **Manager**: npm
- **File**: `package.json`, `package-lock.json`
- **Scripts**: Defined in package.json

### Environment Configuration

#### Go
- **Config Library**: Viper
- **Environment Variables**:
  - `DATABASE_URL` - PostgreSQL connection string
  - `SQLITE_PATH` - SQLite database file
  - `PORT` - HTTP server port
  - `LOG_LEVEL` - Logging verbosity

#### Python
- **Environment Variables**:
  - `KAGENT_URL` - Kagent API endpoint
  - `STS_WELL_KNOWN_URI` - STS service endpoint
  - `LOG_LEVEL` - Logging level

#### Frontend
- **Environment Variables**:
  - `NEXT_PUBLIC_API_URL` - Kagent API endpoint
  - `NEXT_PUBLIC_USER_ID` - User identifier

### Key Dependencies Reference

#### Go (`go.mod`)
```
github.com/gorilla/mux - HTTP router
github.com/spf13/cobra - CLI framework
gorm.io/gorm - ORM
k8s.io/apimachinery - Kubernetes types
github.com/prometheus/client_golang - Metrics
```

#### Python (`pyproject.toml`)
```
google-adk - Google Agent Development Kit
fastapi - HTTP server framework
pydantic - Data validation
httpx - Async HTTP client
openai - OpenAI API
anthropic - Anthropic API
```

#### Frontend (`package.json`)
```
next - React framework
react - UI library
@radix-ui/* - UI primitives
tailwindcss - CSS framework
react-hook-form - Form management
zod - Schema validation
```

### Future Considerations

**Potential Additions:**
- Redis for caching and session storage
- Grafana for visualization
- Sentry for error tracking
- Auth0 or Keycloak for authentication
- S3-compatible storage for artifacts
- Message queue (RabbitMQ, NATS) for async processing
