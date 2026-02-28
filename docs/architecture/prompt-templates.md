# Prompt Templates

This document explains the prompt templates feature — how it works, the design decisions behind it, and how to use it.

## Overview

Prompt templates allow agents to compose their system messages from reusable fragments and dynamic variables, instead of writing everything from scratch. The system message field on the Agent CRD becomes a Go [text/template](https://pkg.go.dev/text/template) when prompt sources are configured, supporting two capabilities:

1. **Include directives** — `{{include "source/key"}}` inserts a prompt fragment from a ConfigMap or Secret
2. **Variable interpolation** — `{{.AgentName}}`, `{{.ToolNames}}`, etc. inject agent metadata

Templates are resolved at **reconciliation time** by the controller. The final, fully-resolved prompt is baked into the agent's config Secret, so the Python ADK runtime receives a plain string with no template syntax.

```text
┌──────────────────────────────────────────────────────────┐
│                   Agent CRD                              │
│                                                          │
│  systemMessage: |                                        │
│    {{include "builtin/skills-usage"}}                    │
│    You are {{.AgentName}}.                               │
│                                                          │
│  promptTemplate:                                         │
│    dataSources:                                          │
│      - kind: ConfigMap                                   │
│        name: kagent-builtin-prompts                      │
│        alias: builtin                                    │
└──────────────────────┬───────────────────────────────────┘
                       │ reconciliation
                       ▼
┌──────────────────────────────────────────────────────────┐
│              Controller (Go)                             │
│                                                          │
│  1. Resolve raw system message                           │
│  2. Fetch all data from referenced ConfigMaps/Secrets    │
│  3. Build lookup map: "alias/key" → value                │
│  4. Build template context from agent metadata           │
│  5. Execute Go text/template with include + variables    │
│  6. Store resolved string in config Secret               │
└──────────────────────┬───────────────────────────────────┘
                       │
                       ▼
┌──────────────────────────────────────────────────────────┐
│           Config Secret → Python ADK                     │
│                                                          │
│  instruction: |                                          │
│    ## Skills                                             │
│    You have access to skills — pre-built capabilities... │
│    You are my-agent.                                     │
└──────────────────────────────────────────────────────────┘
```

## Design Decisions

### Why `source/key` syntax?

ConfigMaps naturally contain multiple keys. Rather than requiring users to declare each key individually as a separate template reference, the `source/key` syntax lets you reference an entire ConfigMap once and access any of its keys directly. This significantly reduces boilerplate when using multiple prompts from the same source.

The `/` separator (rather than `.`) avoids ambiguity since dots are valid in Kubernetes resource names but slashes are not.

### Why aliases?

ConfigMap names can be long (e.g., `kagent-builtin-prompts`). The optional `alias` field lets users define a shorter identifier for use in include directives, keeping the system message readable: `{{include "builtin/skills-usage"}}` instead of `{{include "kagent-builtin-prompts/skills-usage"}}`.

### Why Go text/template?

Prompt engineering is a text-authoring activity. Users need fine-grained control over where fragments are placed within their prose — a structured ordered-parts list would force artificial boundaries. Since the controller is written in Go and already uses `text/template` for other purposes (skills init scripts), it was a natural choice.

### Why resolve at reconciliation time?

Resolving templates in the controller (rather than at runtime in the Python ADK) gives several benefits:

- **Predictability** — the resolved prompt is visible in the config Secret, making debugging straightforward
- **No Python-side changes** — the ADK runtime receives a plain string, keeping the runtime simple
- **Validation** — template errors are caught immediately at reconciliation time and surfaced via the Agent's `Accepted` status condition

### Why no nested includes?

Content pulled from ConfigMaps via `{{include "source/key"}}` is treated as **plain text**, not as a template. This means included fragments cannot themselves use `{{include}}` or `{{.Variable}}` directives. This keeps the system simple, predictable, and avoids potential circular reference issues.

### Why `TypedLocalReference` for prompt sources?

Each prompt source uses an inlined `TypedLocalReference` (`kind`, `apiGroup`, `name`) rather than a fixed enum like `type: ConfigMap`. This makes the API extensible — today it supports ConfigMaps and Secrets (core API group), but a future `PromptLibrary` CRD (`kind: PromptLibrary, apiGroup: kagent.dev`) could be added without changing the schema.

### Why ConfigMaps as the initial implementation?

ConfigMaps are a well-understood Kubernetes primitive and sufficient for the initial implementation. The `TypedLocalReference` pattern ensures the API is ready to support custom CRDs in the future without breaking changes.

### Backwards compatibility

To avoid breaking existing agents that may have literal `{{` characters in their system messages, the `systemMessage` field is **only** treated as a template when `promptTemplate` is set. If no prompt template is configured, the system message is passed through as a plain string, preserving existing behavior.

## Usage

### Basic example

Create a ConfigMap with your prompt fragments, then reference it from your agent:

```yaml
apiVersion: v1
kind: ConfigMap
metadata:
  name: my-prompts
  namespace: default
data:
  preamble: |
    You are a helpful assistant specialized in Kubernetes operations.
    Always explain your reasoning before taking action.
  safety: |
    Never delete resources without explicit user confirmation.
    Never expose secrets or credentials in your responses.
---
apiVersion: kagent.dev/v1alpha2
kind: Agent
metadata:
  name: k8s-helper
  namespace: default
spec:
  description: "Kubernetes troubleshooting agent"
  declarative:
    modelConfig: default-model-config
    systemMessage: |
      {{include "my-prompts/preamble"}}

      Your name is {{.AgentName}} and you operate in the {{.AgentNamespace}} namespace.
      Your purpose: {{.Description}}

      {{include "my-prompts/safety"}}
    promptTemplate:
      dataSources:
        - kind: ConfigMap
          name: my-prompts
    tools:
      - type: McpServer
        mcpServer:
          name: k8s-tools
          kind: RemoteMCPServer
          apiGroup: kagent.dev
          toolNames: ["get-pods", "describe-pod"]
```

This resolves to:

```
You are a helpful assistant specialized in Kubernetes operations.
Always explain your reasoning before taking action.

Your name is k8s-helper and you operate in the default namespace.
Your purpose: Kubernetes troubleshooting agent

Never delete resources without explicit user confirmation.
Never expose secrets or credentials in your responses.
```

### Using aliases

Use the `alias` field to shorten include paths:

```yaml
promptTemplate:
  dataSources:
    - kind: ConfigMap
      name: kagent-builtin-prompts
      alias: builtin
    - kind: ConfigMap
      name: my-custom-prompts
      alias: custom
```

Then in your system message:

```yaml
systemMessage: |
  {{include "builtin/skills-usage"}}
  {{include "custom/my-rules"}}
```

Without aliases, you'd write `{{include "kagent-builtin-prompts/skills-usage"}}`.

### Using built-in prompts

Kagent ships a `kagent-builtin-prompts` ConfigMap (deployed via Helm) with the following keys:

| Key | Description |
|-----|-------------|
| `skills-usage` | Instructions for agents that use skills from `/skills` |
| `tool-usage-best-practices` | Guidelines for effective tool usage |
| `safety-guardrails` | Safety rules for destructive operations and sensitive data |
| `kubernetes-context` | Context about operating within a Kubernetes cluster |
| `a2a-communication` | Guidelines for agent-to-agent communication |

Example using built-in prompts:

```yaml
apiVersion: kagent.dev/v1alpha2
kind: Agent
metadata:
  name: my-agent
  namespace: kagent  # same namespace where kagent is installed
spec:
  description: "A safe Kubernetes agent with skills"
  declarative:
    systemMessage: |
      {{include "builtin/skills-usage"}}
      {{include "builtin/tool-usage-best-practices"}}

      You are {{.AgentName}}. {{.Description}}.

      {{include "builtin/safety-guardrails"}}
      {{include "builtin/kubernetes-context"}}
    promptTemplate:
      dataSources:
        - kind: ConfigMap
          name: kagent-builtin-prompts
          alias: builtin
```

### Using multiple sources

You can reference multiple ConfigMaps and Secrets simultaneously:

```yaml
promptTemplate:
  dataSources:
    - kind: ConfigMap
      name: kagent-builtin-prompts
      alias: builtin
    - kind: ConfigMap
      name: team-prompts
      alias: team
    - kind: Secret
      name: proprietary-prompts
      alias: proprietary
```

Then use any key from any source:

```yaml
systemMessage: |
  {{include "builtin/safety-guardrails"}}
  {{include "team/coding-standards"}}
  {{include "proprietary/company-instructions"}}
```

### Available template variables

When `promptTemplate` is set, the following variables are available in `systemMessage`:

| Variable | Type | Source |
|----------|------|--------|
| `{{.AgentName}}` | `string` | `metadata.name` |
| `{{.AgentNamespace}}` | `string` | `metadata.namespace` |
| `{{.Description}}` | `string` | `spec.description` |
| `{{.ToolNames}}` | `[]string` | Collected from all `tools[*].mcpServer.toolNames` |
| `{{.SkillNames}}` | `[]string` | Derived from `skills.refs` (OCI image name) and `skills.gitRefs[*].name` |

You can use standard Go template constructs to work with these variables:

```yaml
# List tools
Available tools: {{range .ToolNames}}- {{.}}
{{end}}

# Conditional
{{if .SkillNames}}You have skills available.{{end}}

# Count
You have {{len .ToolNames}} tools configured.
```

## ConfigMap change detection

The agent controller watches ConfigMaps referenced by agents via `promptTemplate.dataSources` or `systemMessageFrom`. When a ConfigMap's content changes, all agents referencing it are automatically re-reconciled, and their resolved system messages are updated.

## Error handling

If template resolution fails (missing ConfigMap, invalid template syntax, unknown path in `{{include}}`), the agent's `Accepted` status condition is set to `False` with reason `ReconcileFailed` and a message describing the error. The agent's deployment is not updated until the error is resolved.

```bash
# Check for template errors
kubectl get agent my-agent -o jsonpath='{.status.conditions[?(@.type=="Accepted")]}'
```

## Related Files

- [agent_types.go](../../go/api/v1alpha2/agent_types.go) — `PromptTemplateSpec`, `PromptSource` types
- [template.go](../../go/internal/controller/translator/agent/template.go) — Template resolution logic
- [template_test.go](../../go/internal/controller/translator/agent/template_test.go) — Unit tests
- [adk_api_translator.go](../../go/internal/controller/translator/agent/adk_api_translator.go) — `resolveSystemMessage()` integration
- [agent_controller.go](../../go/internal/controller/agent_controller.go) — ConfigMap watch setup
- [builtin-prompts-configmap.yaml](../../helm/kagent/templates/builtin-prompts-configmap.yaml) — Built-in prompt templates
