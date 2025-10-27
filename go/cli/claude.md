# Kagent CLI

## Overview

The Kagent CLI is a command-line interface and TUI (Text User Interface) for interacting with Kagent agents. It provides tools for installing Kagent, managing agents, developing MCP servers, and chatting with agents through an interactive interface. The CLI is built in Go using the Cobra framework for commands and Bubble Tea for the TUI.

## Architecture

The CLI is structured as a REPL (Read-Eval-Print Loop) with both command-based and interactive modes:
- **Command Mode**: Execute specific commands via CLI arguments
- **Interactive Mode**: TUI-based workspace for agent discovery and chat
- **Agent Management**: Initialize, build, deploy, and invoke agents
- **MCP Development**: Scaffold, build, and deploy MCP servers in multiple languages

## Key Components

### Main Entry Point (`cmd/kagent/main.go`)

#### Root Command
- **Default Behavior**: Launches interactive TUI (line 41: `runInteractive`)
- **Global Flags** (lines 44-48):
  - `--kagent-url`: KAgent API endpoint (default: http://localhost:8083)
  - `--namespace`, `-n`: Kubernetes namespace (default: kagent)
  - `--output-format`, `-o`: Output format (table, json, yaml)
  - `--verbose`, `-v`: Verbose logging
  - `--timeout`: Command timeout (default: 300s)

#### Commands

**Agent Management:**
- `install`: Install Kagent to Kubernetes cluster with optional profiles (minimal, demo)
- `uninstall`: Remove Kagent from cluster
- `invoke`: Execute agent tasks (lines 79-95)
  - Flags: `--agent`, `--task`, `--session`, `--stream`, `--file`
  - Example: `kagent invoke --agent "k8s-agent" --task "Get all pods"`
- `bug-report`: Generate diagnostic information
- `version`: Display CLI version
- `get`: List agents in namespace
- `dashboard`: Open web UI in browser (macOS-specific implementation)

**Agent Development:**
- `init`: Scaffold new agent project (ADK framework)
- `build`: Build agent container image
- `deploy`: Deploy agent to Kubernetes
- `run`: Run agent locally (development mode)
- `add-mcp`: Add MCP server to agent configuration

**MCP Server Development:**
- `mcp init`: Scaffold MCP server in Go, Python, TypeScript, or Java
- `mcp build`: Build MCP server container
- `mcp deploy`: Deploy MCP server to Kubernetes
- `mcp run`: Run MCP server locally
- `mcp inspector`: Interactive tool inspection
- `mcp add-tool`: Add new tool to MCP server
- `mcp secrets`: Manage MCP server secrets

### TUI Components

#### Interactive Workspace (`internal/tui/workspace.go`)
- **Purpose**: Main TUI for agent discovery and interaction
- **Features**:
  - Agent list with filtering
  - Agent selection and details
  - Launch chat sessions
  - Keyboard-driven navigation
- **Framework**: Bubble Tea (charmbracelet)
- **Layout**: Split pane with agent list and details

#### Chat Interface (`internal/tui/chat.go`)
- **Purpose**: Interactive chat with agents (lines 24-30: `RunChat` function)
- **Features**:
  - Message input via textarea
  - Streaming responses from agents
  - Viewport for scrollable conversation history
  - Spinner for working state
  - Session persistence
- **Components** (lines 38-59):
  - `viewport.Model`: Scrollable chat history
  - `textarea.Model`: Message input (line 62-68)
  - `spinner.Model`: Loading indicator (line 75-77)
- **A2A Integration**: Uses A2A protocol for agent communication (line 22: `SendMessageFn`)
- **Event Handling**: Processes streaming events from agents (lines 32-36)

#### Dialogs
- **Agent Chooser** (`internal/tui/dialogs/agent_chooser.go`): Select agent from list
- **MCP Server Wizard** (`internal/tui/dialogs/mcp_server_wizard.go`): Guided MCP server setup
- **Dialog Manager** (`internal/tui/dialogs/manager.go`): Modal dialog system

#### Theme System (`internal/tui/theme/`)
- **Purpose**: Consistent styling across TUI
- **Colors**: Primary, secondary, error, success colors
- **Styles**: Headings, code blocks, borders, highlights

### Agent Operations

#### Agent Framework Support (`internal/agent/frameworks/`)
- **Supported**: ADK (Google Agent Development Kit)
- **Generator** (`frameworks/adk/python/generator.go`): Scaffolds ADK agent projects
- **Base Generator** (`frameworks/common/base_generator.go`): Common generation logic
- **Manifest Manager** (`frameworks/common/manifest_manager.go`): Manages agent.yaml configuration

#### Agent Commands

##### Init Command (`internal/cli/agent/init.go`)
- **Purpose**: Create new agent project from template
- **Flow**:
  1. Prompt for agent name and description
  2. Select framework (ADK)
  3. Generate project structure
  4. Create agent.yaml manifest
  5. Generate Dockerfile and dependencies

##### Build Command (`internal/cli/agent/build.go`)
- **Purpose**: Build agent container image
- **Features**:
  - Docker/Podman support
  - Multi-platform builds
  - Automatic tagging
  - Integration with local registry

##### Deploy Command (`internal/cli/agent/deploy.go`)
- **Purpose**: Deploy agent to Kubernetes
- **Flow**:
  1. Build container image
  2. Push to registry
  3. Apply Kubernetes manifests (Agent CRD)
  4. Wait for deployment readiness

##### Run Command (`internal/cli/agent/run.go`)
- **Purpose**: Local development mode
- **Features**:
  - Run agent outside Kubernetes
  - Hot reload on code changes
  - Local debugging support

##### Invoke Command (`internal/cli/agent/invoke.go`)
- **Purpose**: Execute agent tasks via CLI
- **Features**:
  - One-shot task execution
  - Session management
  - Streaming responses
  - File input support

##### Add MCP Command (`internal/cli/agent/add_mcp.go`)
- **Purpose**: Add MCP server to agent configuration
- **Flow**:
  1. Discover available MCP servers
  2. Select server and tools
  3. Update agent.yaml
  4. Configure credentials if needed

### MCP Operations

#### MCP Framework Support (`internal/mcp/frameworks/`)
- **Languages**: Go, Python, TypeScript, Java
- **Generators**: Language-specific project scaffolding
- **Builder** (`internal/mcp/builder/builder.go`): Unified build interface

#### MCP Commands

##### MCP Init Commands (`internal/cli/mcp/init_*.go`)
- **Purpose**: Create MCP server projects
- **Languages**:
  - Python (`init_python.go`): FastAPI-based MCP server
  - Go (`init_go.go`): Native Go MCP server
  - TypeScript (`init_typescript.go`): Node.js MCP server
  - Java (`init_java.go`): Spring Boot MCP server
- **Features**:
  - Project structure generation
  - Dockerfile creation
  - Dependency management
  - Example tool implementation

##### MCP Build Command (`internal/cli/mcp/build.go`)
- **Purpose**: Build MCP server container
- **Features**:
  - Language-specific build steps
  - Dependency caching
  - Multi-stage Docker builds

##### MCP Deploy Command (`internal/cli/mcp/deploy.go`)
- **Purpose**: Deploy MCP server to Kubernetes
- **Resources**: Creates RemoteMCPServer CRD

##### MCP Run Command (`internal/cli/mcp/run.go`)
- **Purpose**: Local MCP server execution
- **Features**: Development mode with logs

##### MCP Inspector Command (`internal/cli/mcp/inspector.go`)
- **Purpose**: Interactive tool testing
- **Features**:
  - List available tools
  - Test tool execution
  - View schemas

##### MCP Add Tool Command (`internal/cli/mcp/add_tool.go`)
- **Purpose**: Add new tool to existing MCP server
- **Features**: Code generation for tool stubs

##### MCP Secrets Command (`internal/cli/mcp/secrets.go`)
- **Purpose**: Manage MCP server credentials
- **Features**: Create/update Kubernetes secrets

### Configuration

#### Config System (`internal/config/config.go`)
- **Purpose**: Global CLI configuration
- **Settings**:
  - KAgent API URL
  - Default namespace
  - Output format preferences
  - Timeout settings
  - Verbosity level
- **Storage**: User home directory config file

#### Profiles (`internal/profiles/profiles.go`)
- **Purpose**: Installation profiles for different use cases
- **Profiles**:
  - `minimal`: Bare-bones installation
  - `demo`: Full-featured demo setup
- **Usage**: `kagent install --profile=demo`

### Common Utilities

#### Execution Utilities (`internal/common/exec/`)
- **Docker** (`docker.go`): Docker/Podman command execution
- **Kubectl** (`kubectl.go`): Kubernetes operations

#### Filesystem Utilities (`internal/common/fs/`)
- **Project** (`project.go`): Project structure validation and discovery

#### Generator Base (`internal/common/generator/base.go`)
- **Purpose**: Template-based code generation
- **Features**: File generation, variable substitution

#### Image Utilities (`internal/common/image/image.go`)
- **Purpose**: Container image name parsing and validation

#### Kubernetes Config (`internal/common/k8s/config.go`)
- **Purpose**: Kubeconfig management and cluster access

#### Prompt Utilities (`internal/common/prompt/input.go`)
- **Purpose**: Interactive user prompts
- **Features**: Text input, selection lists, confirmations

### Manifest Management

#### MCP Manifests (`internal/mcp/manifests/`)
- **Manager** (`manager.go`): CRUD operations for mcp.yaml files
- **Types** (`types.go`): MCP server configuration schema
- **Fields**: Server name, description, tools, credentials, transport

## Build System

### Multi-Platform Compilation
The CLI is built for multiple platforms:
- Linux: amd64, arm64
- macOS: amd64 (Intel), arm64 (Apple Silicon)
- Windows: amd64

Build outputs in `go/bin/`:
- `kagent-linux-amd64`
- `kagent-linux-arm64`
- `kagent-darwin-amd64`
- `kagent-darwin-arm64`
- `kagent-windows-amd64.exe`

### Build Commands
```bash
# Build all platforms
make build

# Run locally without building
go run cli/cmd/kagent/main.go

# Build for specific platform
GOOS=linux GOARCH=amd64 go build -o bin/kagent-linux-amd64 cli/cmd/kagent/main.go
```

## Usage Examples

### Interactive Mode
```bash
# Launch TUI workspace
kagent

# Select agent from list
# Press Enter to start chat
```

### Command Mode

#### Installation
```bash
# Install with demo profile
kagent install --profile=demo

# Uninstall
kagent uninstall
```

#### Agent Invocation
```bash
# Invoke agent with task
kagent invoke --agent "k8s-agent" --task "List all pods"

# Stream response
kagent invoke --agent "k8s-agent" --task "Analyze errors" --stream

# Use file input
kagent invoke --agent "k8s-agent" --file task.txt
```

#### Agent Development
```bash
# Create new agent
kagent init

# Build agent
kagent build

# Deploy to cluster
kagent deploy

# Run locally
kagent run

# Add MCP server
kagent add-mcp
```

#### MCP Development
```bash
# Create Python MCP server
kagent mcp init

# Build MCP server
kagent mcp build

# Deploy MCP server
kagent mcp deploy

# Test tools
kagent mcp inspector

# Add new tool
kagent mcp add-tool
```

#### Agent Management
```bash
# List agents
kagent get

# Open dashboard
kagent dashboard

# Show version
kagent version

# Generate bug report
kagent bug-report
```

## Key Features

1. **Interactive TUI**: Bubble Tea-based workspace for agent discovery and chat
2. **Multi-Framework Support**: ADK agent scaffolding with extensible framework system
3. **MCP Development**: Full lifecycle support for MCP servers in 4 languages
4. **Streaming Chat**: Real-time agent responses with A2A protocol
5. **Local Development**: Run agents and MCP servers locally without Kubernetes
6. **Kubernetes Integration**: Deploy and manage agents in clusters
7. **Session Management**: Persistent chat sessions across invocations
8. **Cross-Platform**: Builds for Linux, macOS, Windows
9. **Extensible**: Plugin-style architecture for frameworks and generators
10. **Profile-Based Install**: Different installation configurations

## Dependencies

### External Libraries
- **cobra**: CLI framework for commands and flags
- **bubbletea**: TUI framework for interactive interface
- **bubbles**: TUI components (viewport, textarea, spinner)
- **lipgloss**: TUI styling
- **viper**: Configuration management
- **trpc-a2a-go**: A2A protocol client

### Internal Dependencies
- **go/internal/a2a**: A2A protocol implementation
- **go/api/v1alpha2**: Kubernetes CRD definitions
- **go/pkg/client**: Kagent API client

## Configuration Files

### Agent Configuration (agent.yaml)
Generated by `kagent init`:
```yaml
name: my-agent
description: Agent description
framework: adk
version: 0.1.0
tools: []
mcpServers: []
model:
  provider: openai
  name: gpt-4
```

### MCP Server Configuration (mcp.yaml)
Generated by `kagent mcp init`:
```yaml
name: my-mcp-server
description: MCP server description
language: python
version: 0.1.0
tools:
  - name: example_tool
    description: Example tool
transport: stdio
```

## Directory Structure

```
go/cli/
├── cmd/kagent/
│   └── main.go              # Entry point (lines 1-100+)
├── internal/
│   ├── cli/
│   │   ├── agent/           # Agent commands
│   │   │   ├── init.go      # Scaffold agent
│   │   │   ├── build.go     # Build agent image
│   │   │   ├── deploy.go    # Deploy to K8s
│   │   │   ├── run.go       # Local execution
│   │   │   ├── invoke.go    # Execute tasks
│   │   │   └── add_mcp.go   # Add MCP server
│   │   └── mcp/             # MCP commands
│   │       ├── init*.go     # Language-specific scaffolding
│   │       ├── build.go     # Build MCP server
│   │       ├── deploy.go    # Deploy MCP server
│   │       └── inspector.go # Tool testing
│   ├── tui/                 # Terminal UI
│   │   ├── chat.go          # Chat interface (lines 1-100+)
│   │   ├── workspace.go     # Main workspace
│   │   ├── dialogs/         # Modal dialogs
│   │   │   ├── agent_chooser.go
│   │   │   └── mcp_server_wizard.go
│   │   └── theme/           # Styling
│   ├── agent/               # Agent operations
│   │   └── frameworks/      # Framework generators
│   ├── mcp/                 # MCP operations
│   │   ├── frameworks/      # Language generators
│   │   ├── manifests/       # Manifest management
│   │   └── builder/         # Build orchestration
│   ├── config/              # Configuration
│   ├── profiles/            # Install profiles
│   └── common/              # Utilities
│       ├── exec/            # Docker/kubectl
│       ├── fs/              # Filesystem ops
│       ├── generator/       # Code generation
│       ├── image/           # Image utilities
│       ├── k8s/             # K8s config
│       └── prompt/          # User prompts
└── bin/                     # Compiled binaries
```

## Related Components

- **Controller** (`go/internal/controller/`): Kubernetes controller for agent lifecycle
- **HTTP Server** (`go/internal/httpserver/`): REST API for agent management
- **A2A Protocol** (`go/internal/a2a/`): Agent-to-agent communication
- **CRD Definitions** (`go/api/v1alpha2/`): Custom resource types
- **Python Packages** (`python/packages/`): Agent runtime implementations

## Future Enhancements

- **More Frameworks**: Support for LangGraph, CrewAI scaffolding
- **Plugin System**: Third-party framework and command plugins
- **Cloud Providers**: Direct integration with cloud AI services
- **Advanced TUI**: Multi-pane layout, log viewing, metrics
- **Testing Tools**: Unit test generation, integration test runners
- **CI/CD Integration**: GitHub Actions, GitLab CI templates
- **Debugging Tools**: Attach debugger to running agents
- **Agent Marketplace**: Discover and install community agents
