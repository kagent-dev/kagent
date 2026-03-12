# Agent Configuration Reference

## Agent CRD (v1alpha2)

### Declarative Agent — Full Example

```yaml
apiVersion: kagent.dev/v1alpha2
kind: Agent
metadata:
  name: my-agent
  namespace: kagent
spec:
  description: Description shown in dashboard and A2A discovery
  type: Declarative  # or "BYO"

  declarative:
    # LLM model configuration reference
    modelConfig: default-model-config

    # System prompt — defines agent behavior
    systemMessage: |
      You are a helpful Kubernetes operations agent.
      # Instructions
      - Check current state before making changes
      - Explain your reasoning
      - Never delete without confirmation
      # Response Format
      - Use markdown
      - Summarize actions taken

    # Tools the agent can use
    tools:
    - type: McpServer
      mcpServer:
        name: k8s-tools        # name of MCPServer resource
        kind: MCPServer         # MCPServer or RemoteMCPServer
        apiGroup: kagent.dev    # required for BOTH MCPServer and RemoteMCPServer
        toolNames:              # optional: filter to specific tools
          - k8s_get_resources
          - k8s_get_pods

    # A2A configuration for agent-to-agent communication
    a2aConfig:
      skills:
      - id: k8s-query
        name: Kubernetes Query
        description: Query and inspect Kubernetes resources
        tags: ["kubernetes", "query"]
        examples:
        - "What pods are running in the default namespace?"
        - "Show me all services"

    # Deployment customization
    deployment:
      replicas: 1
      imagePullPolicy: IfNotPresent
      resources:
        limits:
          cpu: "1"
          memory: 1Gi
        requests:
          cpu: 100m
          memory: 256Mi
      env:
      - name: LOG_LEVEL
        value: info

  # Container-based skills (note: at spec level, not under declarative)
  skills:
    refs:
    - ghcr.io/my-org/my-skill:latest
```

### BYO Agent — Full Example

```yaml
apiVersion: kagent.dev/v1alpha2
kind: Agent
metadata:
  name: my-custom-agent
  namespace: kagent
spec:
  description: Custom agent built with Python ADK
  type: BYO

  byo:
    deployment:
      image: my-registry/my-agent:latest
      replicas: 1
      env:
      - name: OPENAI_API_KEY
        valueFrom:
          secretKeyRef:
            name: llm-credentials
            key: api-key
      resources:
        limits:
          cpu: "2"
          memory: 2Gi
```

## RemoteMCPServer Resource

Connects to an MCP server (external or in-cluster) via network.

```yaml
apiVersion: kagent.dev/v1alpha2
kind: RemoteMCPServer
metadata:
  name: external-tools
  namespace: kagent
spec:
  description: External MCP tool server
  url: http://external-mcp.example.com:3000/sse
  protocol: SSE              # SSE or STREAMABLE_HTTP (default)
  # timeout: 30s             # optional request timeout
  # sseReadTimeout: 60s      # optional SSE read timeout
  # terminateOnClose: true   # default true
  # headersFrom:             # optional headers from secrets
  # - name: Authorization
  #   valueFrom:
  #     secretKeyRef:
  #       name: my-secret
  #       key: token
```

### Referencing a RemoteMCPServer in an Agent

When adding a RemoteMCPServer to an agent's tool list, always include `apiGroup: kagent.dev`:

```yaml
tools:
- type: McpServer
  mcpServer:
    name: external-tools
    kind: RemoteMCPServer
    apiGroup: kagent.dev          # required — omitting causes reconciliation issues
    toolNames:                    # optional: limit to specific tools
      - my_tool
```

## MCPServer Resource (KMCP)

MCPServer resources are managed by the KMCP controller (included with kagent since v0.7). These deploy and manage MCP server pods directly in the cluster. See the kmcp documentation for details on creating MCPServer resources.

## ModelConfig Resource

Configures the LLM provider and model.

```yaml
apiVersion: kagent.dev/v1alpha2
kind: ModelConfig
metadata:
  name: default-model-config
  namespace: kagent
spec:
  provider: OpenAI       # OpenAI, Anthropic, AzureOpenAI, Gemini, GeminiVertexAI, AnthropicVertexAI, Ollama, Bedrock
  model: gpt-4.1-mini
  apiKeySecret: llm-api-key      # name of the K8s Secret
  apiKeySecretKey: api-key       # key within the Secret
  # apiKeyPassthrough: false     # pass API key from request headers
  # defaultHeaders: {}           # custom headers for all requests
  # tls:                         # TLS configuration
  #   disableVerify: false
```

## System Prompt Design

### Progressive Complexity Approach

**Level 1 — Role definition:**
```
You are a Kubernetes operations agent that helps manage clusters.
```

**Level 2 — Add tool documentation:**
```
You are a Kubernetes operations agent.

## Available Tools
- k8s_get_resources: List resources of a given type. Use when user asks about pods, services, etc.
- k8s_apply_manifest: Apply a YAML manifest. Use for creating or updating resources.
```

**Level 3 — Add behavioral guidelines:**
```
You are a Kubernetes operations agent.

## Tools
[tool descriptions]

## Behavior
- Always check current resource state before making changes
- For destructive operations (delete, scale down), ask for confirmation
- If a command fails, diagnose the issue before retrying
- Format responses in markdown with clear sections

## Safety
- Never run commands that could cause data loss without explicit approval
- Prefer read operations first, then mutations
- Always explain what a command will do before executing
```

### Prompt Templates

kagent supports Go `text/template` syntax in `systemMessage` for composing prompts from reusable parts stored in ConfigMaps. Add a `promptTemplate` field with `dataSources` to enable template processing.

**Include content from ConfigMaps:**
```yaml
spec:
  declarative:
    systemMessage: |
      {{include "builtin/safety-guardrails"}}

      You are {{.AgentName}} in namespace {{.AgentNamespace}}.
      {{.Description}}

      ## Tools
      {{range .ToolNames}}- {{.}}
      {{end}}

      {{include "team/response-format"}}
    promptTemplate:
      dataSources:
      - kind: ConfigMap
        name: kagent-builtin-prompts
        alias: builtin
      - kind: ConfigMap
        name: my-team-prompts
        alias: team
```

**Include syntax:** `{{include "alias/key"}}` — `alias` is the datasource alias (or ConfigMap name if no alias), `key` is the ConfigMap data key. Separator is `/` (not `.`, since `.` is valid in K8s names).

**Template variables:**

| Variable | Source |
|----------|--------|
| `{{.AgentName}}` | `metadata.name` |
| `{{.AgentNamespace}}` | `metadata.namespace` |
| `{{.Description}}` | `spec.description` |
| `{{.ToolNames}}` | Tool names from translated config |
| `{{.SkillNames}}` | Skill names from `skills.refs` |

**Storing the entire prompt in a ConfigMap:**

Use `systemMessageFrom` instead of `systemMessage` to load the template from a ConfigMap key:

```yaml
apiVersion: v1
kind: ConfigMap
metadata:
  name: agent-prompts
  namespace: kagent
data:
  k8s-agent-prompt: |
    {{include "builtin/safety-guardrails"}}
    You are a Kubernetes expert...
---
spec:
  declarative:
    systemMessageFrom:
      type: ConfigMap
      name: agent-prompts
      key: k8s-agent-prompt
    promptTemplate:
      dataSources:
      - kind: ConfigMap
        name: kagent-builtin-prompts
        alias: builtin
```

Note: `systemMessage` and `systemMessageFrom` are mutually exclusive.

**Built-in prompt library** (`kagent-builtin-prompts` ConfigMap, ships with kagent):

| Key | Content |
|-----|---------|
| `skills-usage` | Instructions for agents with skills |
| `tool-usage-best-practices` | Read before write, explain before acting, verify after changes |
| `safety-guardrails` | No destructive ops without confirmation, least privilege, rollback |
| `kubernetes-context` | Investigation protocol, problem-solving framework |
| `a2a-communication` | Agent-to-agent delegation guidelines |

**Key behaviors:**
- Templates only activate when `promptTemplate` is set (backwards compatible — literal `{{` preserved otherwise)
- Included content is plain text (no nested template execution)
- ConfigMap changes trigger automatic agent re-reconciliation
- Template errors surface in the Agent's `Accepted` status condition
- Resolved at reconciliation time, not runtime — the agent receives a plain string

## Built-in Agents (demo profile)

When installed with `--profile demo`, kagent includes:
- **k8s-agent**: Kubernetes resource management
- **helm-agent**: Helm chart operations
- **istio-agent**: Istio service mesh management
- **kgateway-agent**: Gateway API troubleshooting
- **argo-rollouts-agent**: Progressive delivery
- **cilium agents**: CNI debugging, management, policy
- **observability-agent**: Prometheus/Grafana queries
- **promql-agent**: PromQL query assistance
