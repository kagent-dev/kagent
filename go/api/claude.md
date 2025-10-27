# Kubernetes CRD API Definitions (v1alpha2)

## Overview

The `go/api/v1alpha2` package defines Kubernetes Custom Resource Definitions (CRDs) for Kagent. These types represent the declarative API for managing agents, model configurations, and MCP servers in Kubernetes. The package uses kubebuilder markers for code generation and validation.

## Architecture

The API follows Kubernetes API conventions:
- **Spec**: Desired state defined by users
- **Status**: Observed state maintained by controllers
- **Validation**: Kubebuilder markers for OpenAPI schema generation
- **Versioning**: v1alpha2 indicates evolving API (not yet stable)

## Core Custom Resources

### 1. Agent (`agent_types.go`)

#### AgentSpec (lines 44-56)
The root specification for an Agent resource.

##### Fields
- **Type**: `AgentType` enum - Either "Declarative" or "BYO" (Bring Your Own)
  - Default: Declarative
  - Validation: Must be specified, must match one of the enum values
- **Declarative**: `*DeclarativeAgentSpec` - Configuration for managed agents
- **BYO**: `*BYOAgentSpec` - Configuration for custom agent containers
- **Description**: Human-readable agent description

##### Validation Rules (lines 41-43)
- Type must be specified
- Type must be Declarative or BYO
- If Declarative, declarative field must be specified
- If BYO, byo field must be specified

#### DeclarativeAgentSpec (lines 59-88)
Configuration for Kagent-managed agents using built-in frameworks (ADK, LangGraph, CrewAI).

##### Fields
- **SystemMessage**: String containing system prompt
- **SystemMessageFrom**: Reference to ConfigMap/Secret for system prompt
  - Mutually exclusive with SystemMessage (line 58 validation)
- **ModelConfig**: Name of ModelConfig resource to use
  - Default: "default-model-config"
  - Must be in same namespace
- **Stream**: Whether to stream responses (default: true)
- **Tools**: Array of Tool definitions (max 20 items)
- **A2AConfig**: A2A server configuration for agent endpoints
- **Deployment**: Deployment specification for agent pod

#### BYOAgentSpec (lines 97-101)
Configuration for user-provided agent containers.

##### Fields
- **Deployment**: Deployment specification with custom image

#### DeclarativeDeploymentSpec (lines 90-95)
Deployment settings for declarative agents.

##### Fields
- **ImageRegistry**: Custom container registry for agent images
- **SharedDeploymentSpec**: Common deployment settings (inline)

#### ByoDeploymentSpec (lines 103-112)
Deployment settings for BYO agents.

##### Fields
- **Image**: Container image URL (required, min length 1)
- **Cmd**: Override container command
- **Args**: Container arguments
- **SharedDeploymentSpec**: Common deployment settings (inline)

#### SharedDeploymentSpec (lines 114-136)
Common deployment configuration shared by both agent types.

##### Fields
- **Replicas**: Number of pod replicas (default: 1, minimum: 1)
- **ImagePullSecrets**: Credentials for private registries
- **Volumes**: Kubernetes volumes to mount
- **VolumeMounts**: Volume mount points in container
- **Labels**: Pod labels
- **Annotations**: Pod annotations
- **Env**: Environment variables
- **ImagePullPolicy**: Image pull strategy (Always, IfNotPresent, Never)
- **Resources**: CPU/memory requests and limits

#### Tool Type (lines 138-150)
Represents a tool available to the agent.

##### Fields
- **Type**: `ToolProviderType` enum - "McpServer" or "Agent"
- **McpServer**: Reference to MCP server (if type is McpServer)
- **Agent**: Reference to agent (if type is Agent)

##### Validation Rules
- McpServer field must be nil if type is not McpServer
- McpServer must be specified if type is McpServer
- Agent field must be nil if type is not Agent
- Agent must be specified if type is Agent

#### AgentType Enum (lines 31-38)
```go
const (
    AgentType_Declarative AgentType = "Declarative"
    AgentType_BYO         AgentType = "BYO"
)
```

### 2. Common Types (`common_types.go`)

#### ValueSource (lines 36-58)
Represents a value sourced from ConfigMap or Secret.

##### Fields
- **Type**: `ValueSourceType` enum - "ConfigMap" or "Secret"
- **Name**: Name of the ConfigMap or Secret
- **Key**: Key within the ConfigMap/Secret data

##### Methods
- **Resolve()**: Fetches actual value from Kubernetes (lines 45-58)
  - Parameters: context, client, namespace
  - Returns: resolved string value
  - Errors: if resource not found or type unknown

#### ValueRef (lines 60-88)
Represents a configuration value that can be literal or referenced.

##### Fields
- **Name**: Configuration parameter name
- **Value**: Literal value (optional)
- **ValueFrom**: Reference to ConfigMap/Secret (optional)

##### Validation (line 61)
Exactly one of `value` or `valueFrom` must be specified (mutually exclusive).

##### Methods
- **Resolve()**: Gets the value, either directly or from reference (lines 70-88)
  - Returns: (name, value, error) tuple
  - Handles both literal and referenced values

#### ValueSourceType Enum (lines 28-33)
```go
const (
    ConfigMapValueSource ValueSourceType = "ConfigMap"
    SecretValueSource    ValueSourceType = "Secret"
)
```

### 3. ModelConfig (`modelconfig_types.go`)

#### ModelProvider Enum (lines 27-39)
Supported LLM providers:
```go
const (
    ModelProviderAnthropic         ModelProvider = "Anthropic"
    ModelProviderAzureOpenAI       ModelProvider = "AzureOpenAI"
    ModelProviderOpenAI            ModelProvider = "OpenAI"
    ModelProviderOllama            ModelProvider = "Ollama"
    ModelProviderGemini            ModelProvider = "Gemini"
    ModelProviderGeminiVertexAI    ModelProvider = "GeminiVertexAI"
    ModelProviderAnthropicVertexAI ModelProvider = "AnthropicVertexAI"
)
```

#### Provider-Specific Configurations

##### BaseVertexAIConfig (lines 41-65)
Common configuration for Google Vertex AI services.

**Fields:**
- ProjectID (required)
- Location (required)
- Temperature (optional)
- TopP (optional)
- TopK (optional)
- StopSequences (optional)

##### GeminiVertexAIConfig (lines 67-82)
Gemini-specific Vertex AI settings.

**Additional Fields:**
- MaxOutputTokens
- CandidateCount
- ResponseMimeType
- Inherits BaseVertexAIConfig

##### AnthropicVertexAIConfig (lines 84-90)
Anthropic-specific Vertex AI settings.

**Additional Fields:**
- MaxTokens
- Inherits BaseVertexAIConfig

##### AnthropicConfig (lines 92-100)
Direct Anthropic API configuration.

**Fields:**
- BaseURL (optional, overrides default)
- MaxTokens (optional)

### 4. RemoteMCPServer (`remotemcpserver_types.go`)

#### RemoteMCPServerSpec (lines 38-80)
Defines a remote Model Context Protocol server.

##### Fields
- **Description**: Human-readable description
- **Protocol**: `RemoteMCPServerProtocol` enum
  - Default: "STREAMABLE_HTTP"
  - Options: "SSE" or "STREAMABLE_HTTP"
- **URL**: Server endpoint (required, min length 1)
- **HeadersFrom**: Array of ValueRef for HTTP headers (e.g., auth tokens)
- **Timeout**: Request timeout duration
- **SseReadTimeout**: SSE-specific read timeout
- **TerminateOnClose**: Whether to terminate connection on close (default: true)

##### Methods
- **ResolveHeaders()**: Resolves all header references to actual values (lines 67-80)
  - Returns: map[string]string of resolved headers
  - Handles ConfigMap/Secret lookups
- **Scan()**: SQL scanner interface for database storage (lines 59-65)
- **Value()**: SQL valuer interface for database storage (lines 84-86)

#### RemoteMCPServerStatus (lines 88-96)
Observed state of MCP server.

##### Fields
- **ObservedGeneration**: Last processed spec generation
- **Conditions**: Status conditions (ready, progressing, degraded)
- **DiscoveredTools**: Array of MCPTool discovered from server

#### MCPTool (lines 98-100+)
Represents a tool exposed by MCP server.

##### Fields
- **Name**: Tool identifier
- **Description**: Tool description

#### RemoteMCPServerProtocol Enum (lines 31-36)
```go
const (
    RemoteMCPServerProtocolSse            RemoteMCPServerProtocol = "SSE"
    RemoteMCPServerProtocolStreamableHttp RemoteMCPServerProtocol = "STREAMABLE_HTTP"
)
```

## Kubebuilder Markers

### Validation Markers
- `+kubebuilder:validation:Enum`: Restricts field to enum values
- `+kubebuilder:validation:MinLength`: Minimum string length
- `+kubebuilder:validation:MaxItems`: Maximum array size
- `+kubebuilder:validation:Minimum`: Minimum numeric value
- `+kubebuilder:validation:XValidation`: Custom CEL validation expressions
- `+kubebuilder:validation:Optional`: Field is optional

### Default Values
- `+kubebuilder:default=<value>`: Sets default value in CRD schema

### RBAC Markers
- `+kubebuilder:rbac`: Defines RBAC permissions for controllers

### Status Subresource
- `+kubebuilder:subresource:status`: Enables status subresource

### Conditions
- `+kubebuilder:printcolumn`: Defines columns for `kubectl get`

## Usage Examples

### Declarative Agent
```yaml
apiVersion: kagent.dev/v1alpha2
kind: Agent
metadata:
  name: my-agent
  namespace: kagent
spec:
  type: Declarative
  description: "My AI agent"
  declarative:
    systemMessage: "You are a helpful assistant"
    modelConfig: default-model-config
    stream: true
    tools:
      - type: McpServer
        mcpServer:
          name: github-mcp
          namespace: kagent
    deployment:
      replicas: 2
      resources:
        requests:
          cpu: 100m
          memory: 256Mi
```

### BYO Agent
```yaml
apiVersion: kagent.dev/v1alpha2
kind: Agent
metadata:
  name: custom-agent
spec:
  type: BYO
  byo:
    deployment:
      image: myregistry/my-agent:latest
      replicas: 1
      env:
        - name: API_KEY
          valueFrom:
            secretKeyRef:
              name: api-keys
              key: openai
```

### ModelConfig
```yaml
apiVersion: kagent.dev/v1alpha2
kind: ModelConfig
metadata:
  name: my-model-config
spec:
  provider: OpenAI
  model: gpt-4
  apiKey:
    valueFrom:
      type: Secret
      name: openai-secret
      key: api-key
  temperature: "0.7"
  maxTokens: 2000
```

### RemoteMCPServer
```yaml
apiVersion: kagent.dev/v1alpha2
kind: RemoteMCPServer
metadata:
  name: github-mcp
spec:
  description: "GitHub MCP Server"
  protocol: STREAMABLE_HTTP
  url: "https://github-mcp.example.com"
  headersFrom:
    - name: Authorization
      valueFrom:
        type: Secret
        name: github-token
        key: token
  timeout: 30s
```

## Key Design Patterns

### Mutually Exclusive Fields
Used extensively for type-safe configuration:
- `systemMessage` XOR `systemMessageFrom`
- `value` XOR `valueFrom`
- `declarative` XOR `byo` (based on type)

### Inline Composition
Shared configuration embedded via `,inline` tag:
- `SharedDeploymentSpec` reused in both Declarative and BYO

### Value Resolution
Two-step pattern for ConfigMap/Secret references:
1. Define reference in spec (ValueSource, ValueRef)
2. Resolve at runtime via `Resolve()` methods

### Database Integration
MCP server specs are stored in database:
- Implements `sql.Scanner` for reading
- Implements `driver.Valuer` for writing
- JSON marshaling for storage

## Validation Strategy

### Kubebuilder CEL Validation
Custom validation expressions using Common Expression Language:
- Type consistency checks
- Field presence validation
- Mutual exclusivity enforcement
- Complex business logic validation

### OpenAPI Schema Generation
Kubebuilder markers generate OpenAPI schemas for:
- API documentation
- Client-side validation
- kubectl validation
- Admission webhook validation

## Related Components

- **Controller** (`go/internal/controller/`): Reconciles these CRDs
- **Translator** (`go/internal/controller/translator/`): Converts CRDs to Kubernetes resources
- **Database** (`go/internal/database/`): Persists CRD data
- **HTTP Server** (`go/internal/httpserver/`): Serves REST API for CRDs

## API Evolution

### Version: v1alpha2
- **Status**: Alpha (breaking changes expected)
- **Stability**: Evolving API surface
- **Migration**: May require manual updates between versions

### Future Versions
- **v1alpha3**: Potential next iteration
- **v1beta1**: Stable API with deprecation policy
- **v1**: Production-ready, backward-compatible

## Code Generation

### Required Targets
After modifying types, run:
```bash
# Generate deepcopy, clientset, informers, listers
make generate

# Generate CRD manifests
make manifests
```

### Generated Files
- `zz_generated.deepcopy.go`: Deep copy methods
- `config/crd/bases/*.yaml`: CRD manifests

## Best Practices

1. **Always use validation markers**: Prevent invalid configurations
2. **Provide defaults**: Improve user experience
3. **Document fields**: Add comments for godoc and schema
4. **Test validation**: Ensure CEL expressions work
5. **Version carefully**: Plan for API evolution
6. **Maintain backward compatibility**: Within same version
7. **Use status conditions**: Report reconciliation state
8. **Implement deep copy**: Required for all types

## Dependencies

### External
- `k8s.io/apimachinery`: Kubernetes API machinery
- `k8s.io/api`: Standard Kubernetes types (corev1)
- `sigs.k8s.io/controller-runtime`: Controller runtime interfaces
- `trpc-a2a-go/server`: A2A protocol types

### Internal
- `go/internal/utils`: Utility functions for value resolution

## Package Structure

```
go/api/v1alpha2/
├── agent_types.go              # Agent CRD (lines 1-150+)
├── common_types.go             # Shared types (lines 1-89)
├── modelconfig_types.go        # ModelConfig CRD (lines 1-100+)
├── remotemcpserver_types.go    # RemoteMCPServer CRD (lines 1-100+)
├── groupversion_info.go        # API group metadata
└── zz_generated.deepcopy.go    # Generated deepcopy methods
```
