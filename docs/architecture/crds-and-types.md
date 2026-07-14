# CRDs and Type System

This document details all Custom Resource Definitions in kagent and how their types flow through the system.

## CRD Overview

```
┌──────────────────┐       ┌──────────────────┐
│      Agent       │──────▶│   ModelConfig     │
│  (kagent.dev)    │ refs  │   (kagent.dev)    │
└────────┬─────────┘       └──────────────────┘
         │ refs
         ▼
┌──────────────────┐       ┌──────────────────┐
│ RemoteMCPServer  │       │    MCPServer      │
│  (kagent.dev)    │       │   (kmcp.io)       │
└──────────────────┘       └──────────────────┘
```

**All CRDs use API version `kagent.dev/v1alpha2`** (except MCPServer which is from KMCP).

---

## Agent CRD

**File:** `go/api/v1alpha2/agent_types.go`

The central CRD. Defines an AI agent's configuration, tools, and deployment.

### Spec Hierarchy

```
AgentSpec
├── type: Declarative | BYO
├── description: string
├── iconUrl: string (surfaced on the A2A AgentCard)
├── documentationUrl: string (surfaced on the A2A AgentCard)
├── version: string (surfaced on the A2A AgentCard)
├── provider: AgentProvider (surfaced on the A2A AgentCard)
│   ├── organization: string
│   └── url: string
├── skills: SkillForAgent
│   ├── refs: []string (OCI image refs)
│   ├── gitRefs: []GitRepo
│   └── gitAuthSecretRef: LocalObjectReference
├── allowedNamespaces: AllowedNamespaces
│
├── declarative: DeclarativeAgentSpec (if type=Declarative)
│   ├── runtime: python | go
│   ├── systemMessage: string (or Go template if promptTemplate set)
│   ├── systemMessageFrom: ValueSource (alternative: load from ConfigMap/Secret)
│   ├── promptTemplate: PromptTemplateSpec
│   │   └── dataSources: []PromptSource
│   │       ├── kind: ConfigMap
│   │       ├── name: string
│   │       └── alias: string
│   ├── modelConfig: string (name of ModelConfig in same namespace)
│   ├── stream: bool
│   ├── tools: []Tool
│   │   ├── type: McpServer | Agent
│   │   ├── mcpServer: McpServerTool
│   │   │   ├── TypedReference (kind, apiGroup, name, namespace)
│   │   │   ├── toolNames: []string
│   │   │   ├── requireApproval: []string (subset of toolNames)
│   │   │   └── allowedHeaders: []string
│   │   ├── agent: TypedReference (for agent-to-agent)
│   │   └── headersFrom: []ValueRef
│   ├── a2aConfig: A2AConfig
│   │   └── skills: []AgentSkill
│   ├── deployment: DeclarativeDeploymentSpec
│   │   ├── imageRegistry: string
│   │   └── SharedDeploymentSpec (replicas, volumes, env, resources, etc.)
│   ├── memory: MemorySpec
│   │   ├── modelConfig: string (embedding model)
│   │   └── ttlDays: int
│   ├── context: ContextConfig
│   │   └── compaction: ContextCompressionConfig
│   │       ├── compactionInterval: int
│   │       ├── overlapSize: int
│   │       ├── summarizer: ContextSummarizerConfig
│   │       ├── tokenThreshold: int
│   │       └── eventRetentionSize: int
│   ├── reliability: ReliabilityConfig
│   │   ├── toolRetries: int (reflect-and-retry on failed tool calls)
│   │   ├── maxLLMCalls: int (cap on model calls per request)
│   │   └── debugLogging: bool (log every LLM request/response and tool call)
│   └── executeCodeBlocks: bool (currently ignored)
│
└── byo: BYOAgentSpec (if type=BYO)
    └── deployment: ByoDeploymentSpec
        ├── image: string
        ├── cmd: string
        ├── args: []string
        └── SharedDeploymentSpec (replicas, volumes, env, resources, etc.)
```

### Status

```
AgentStatus
├── observedGeneration: int64
└── conditions: []metav1.Condition
    ├── type: "Accepted" (CRD spec is valid)
    └── type: "Ready" (agent pod is running and healthy)
```

### Key Validation Rules (CEL)

- `type` must be `Declarative` or `BYO`
- If `type=Declarative`, `declarative` must be set; if `type=BYO`, `byo` must be set
- `systemMessage` and `systemMessageFrom` are mutually exclusive
- `serviceAccountName` and `serviceAccountConfig` are mutually exclusive
- `requireApproval` entries must be a subset of `toolNames`

---

## ModelConfig CRD

**File:** `go/api/v1alpha2/modelconfig_types.go`

Configures LLM provider credentials and model parameters.

### Spec

```
ModelConfigSpec
├── model: string (e.g. "gpt-4o", "claude-sonnet-4-5-20250514")
├── provider: Anthropic | OpenAI | AzureOpenAI | Ollama | Gemini | GeminiVertexAI | AnthropicVertexAI | Bedrock
├── apiKeySecret: string (Secret name)
├── apiKeySecretKey: string (key within Secret)
├── apiKeyPassthrough: bool (use Bearer token from A2A request)
├── defaultHeaders: map[string]string
├── tls: TLSConfig
│   ├── disableVerify: bool
│   ├── caCertSecretRef: string
│   ├── caCertSecretKey: string
│   └── disableSystemCAs: bool
│
├── retry: ModelRetryConfig
│   └── attempts: int (max retries of failed LLM HTTP requests with exponential backoff;
│                      OpenAI/AzureOpenAI/Anthropic/Gemini only)
│
├── openAI: OpenAIConfig
│   ├── baseUrl, temperature, maxTokens, topP
│   ├── frequencyPenalty, presencePenalty
│   ├── seed, n, timeout
│   └── reasoningEffort: minimal | low | medium | high
├── anthropic: AnthropicConfig
│   └── baseUrl, maxTokens, temperature, topP, topK
├── azureOpenAI: AzureOpenAIConfig
│   └── azureEndpoint, apiVersion, azureDeployment, etc.
├── ollama: OllamaConfig
│   └── host, options
├── gemini: GeminiConfig
├── geminiVertexAI: GeminiVertexAIConfig
│   └── projectID, location, temperature, maxOutputTokens, etc.
├── anthropicVertexAI: AnthropicVertexAIConfig
│   └── projectID, location, temperature, maxTokens, etc.
└── bedrock: BedrockConfig
    └── region
```

### Key Validation Rules

- Provider-specific config (e.g. `openAI`) must only be set when provider matches
- `apiKeyPassthrough` and `apiKeySecret` are mutually exclusive
- `apiKeyPassthrough` not allowed for Gemini/VertexAI providers
- TLS `caCertSecretRef` and `caCertSecretKey` must be set together

---

## RemoteMCPServer CRD

**File:** `go/api/v1alpha2/remotemcpserver_types.go`

Declares a remote MCP tool server that agents can reference.

### Spec

```
RemoteMCPServerSpec
├── description: string
├── protocol: SSE | STREAMABLE_HTTP (default: STREAMABLE_HTTP)
├── url: string (e.g. "http://my-server:8084/mcp")
├── headersFrom: []ValueRef (headers resolved from Secrets/ConfigMaps)
├── timeout: Duration
├── sseReadTimeout: Duration
├── terminateOnClose: bool (default: true)
└── allowedNamespaces: AllowedNamespaces
```

### Status

```
RemoteMCPServerStatus
├── observedGeneration: int64
├── conditions: []metav1.Condition
│   └── type: "Accepted"
└── discoveredTools: []MCPTool
    ├── name: string
    └── description: string
```

When reconciled, the controller connects to the MCP server, lists tools, and populates `discoveredTools`.

---

## Common Types

**File:** `go/api/v1alpha2/common_types.go`

### ValueRef

Resolves a value from a Secret or ConfigMap key:

```go
type ValueRef struct {
    Kind     string // "Secret" or "ConfigMap"
    Name     string // resource name
    Key      string // key within the resource
    ApiGroup string // usually "" for core
}
```

Used for `headersFrom` on both Agent tools and RemoteMCPServer.

### AllowedNamespaces

Controls cross-namespace references (follows Gateway API pattern):

```go
type AllowedNamespaces struct {
    From     AllowedNamespacesFrom // "All" or "Selector"
    Selector *metav1.LabelSelector // when From="Selector"
}
```

### TypedReference / TypedLocalReference

```go
type TypedReference struct {
    Kind      string // e.g. "RemoteMCPServer", "Agent"
    ApiGroup  string // e.g. "kagent.dev"
    Name      string
    Namespace string // optional, for cross-namespace
}

type TypedLocalReference struct {
    Kind     string
    ApiGroup string
    Name     string
}
```

---

## Type Flow: CRD → Go ADK → Python ADK

The same configuration data flows through three type systems:

```
CRD Types (Go)                    Go ADK Types              Python ADK Types
go/api/v1alpha2/                  go/adk/types.go           kagent/adk/types.py
────────────────                  ──────────────            ────────────────────
AgentSpec                    ──▶  AgentConfig          ──▶  AgentConfig
DeclarativeAgentSpec         ──▶  AgentConfig.Agent    ──▶  AgentConfig.agent
ModelConfigSpec              ──▶  ModelConfig          ──▶  ModelConfig
McpServerTool + RemoteMCPServer ──▶ HttpMcpServerConfig ──▶ HttpMcpServerConfig
                                    SseMcpServerConfig      SseMcpServerConfig
```

The **translator** (`go/core/internal/controller/translator/agent/adk_api_translator.go`) converts CRD types to Go ADK types. These are serialized to JSON as `config.json`. The Python ADK deserializes them using Pydantic models that mirror the Go struct tags.

---

## Database Models

**File:** `go/api/database/models.go`

The database models are separate from CRD types and optimized for querying:

```
Agent (DB)
├── ID: string
├── Name: string
├── Namespace: string
├── Type: string (Declarative/BYO)
├── Description: string
├── Config: JSON (serialized AgentConfig)
└── CreatedAt, UpdatedAt

ToolServer (DB)
├── Name: string
├── GroupKind: string
├── Description: string
├── LastConnected: timestamp
└── CreatedAt, UpdatedAt

Tool (DB)
├── ID: string (tool name)
├── ServerName: string (FK to ToolServer)
├── GroupKind: string
├── Description: string
└── CreatedAt, UpdatedAt

Session (DB)
├── ID: string
├── UserID: string
├── AgentID: string
├── Name: string
└── CreatedAt, UpdatedAt

Task (DB)
├── ID: string
├── SessionID: string (FK to Session)
├── Data: JSON (A2A protocol.Message)
└── CreatedAt, UpdatedAt

Event (DB)
├── ID: string
├── SessionID: string (FK to Session)
├── UserID: string
├── Data: JSON (protocol.Message)
└── CreatedAt, UpdatedAt

Feedback (DB)
├── UserID: string
├── MessageID: string
├── IsPositive: bool
├── FeedbackText: string
├── IssueType: string
└── CreatedAt, UpdatedAt

Memory (DB)
├── ID: string
├── AgentName: string
├── UserID: string
├── Content: string
├── Embedding: vector (pgvector)
├── ExpiresAt: timestamp
└── CreatedAt, UpdatedAt
```

---

## Adding a New CRD Field: Checklist

When adding a field to an existing CRD, update all layers:

1. **CRD type** — `go/api/v1alpha2/*_types.go` (add field with kubebuilder markers)
2. **Code generation** — `make -C go generate` (DeepCopy, CRD manifests)
3. **Helm CRD chart** — `cp go/api/config/crd/bases/*.yaml helm/kagent-crds/templates/`
4. **Go ADK types** — `go/adk/types.go` (if field affects agent config)
5. **Translator** — `go/core/internal/controller/translator/agent/adk_api_translator.go` (wire field into config)
6. **Python ADK types** — `python/packages/kagent-adk/src/kagent/adk/types.py` (mirror Go types)
7. **Python runtime** — Use the field in agent setup if it affects runtime behavior
8. **Go runtime** — `go/adk/pkg/` (mirror runtime behavior for `runtime: go` agents)
9. **Tests** — Translator unit tests (golden files), E2E tests
10. **Helm values** — If exposed to users installing via Helm

See [controller-reconciliation.md](controller-reconciliation.md) for the reconciliation flow and the kagent-dev skill for step-by-step examples.
