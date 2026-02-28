# Implementation Plan: Built-in Prompts / Prompt Templates (#1401)

## Overview

Add Go `text/template` support to the Agent CRD's `systemMessage` field, enabling composable prompt fragments (`{{include "name"}}`) and variable interpolation (`{{.AgentName}}`). Templates are resolved at reconciliation time by the controller. Built-in prompts are shipped as a Helm ConfigMap.

---

## Step 1: CRD Changes

**File:** `go/api/v1alpha2/agent_types.go`

Add `PromptTemplates` field to `DeclarativeAgentSpec`:

```go
type DeclarativeAgentSpec struct {
    // ... existing fields ...

    // PromptTemplates defines named prompt fragments that can be referenced
    // in systemMessage using Go template syntax: {{include "name"}}.
    // When this field is non-empty, systemMessage is treated as a Go template
    // with access to include() and agent context variables.
    // +optional
    PromptTemplates []PromptTemplateRef `json:"promptTemplates,omitempty"`
}

type PromptTemplateRef struct {
    // Name is the template identifier used in {{include "name"}} directives.
    // +kubebuilder:validation:Required
    // +kubebuilder:validation:MinLength=1
    Name string `json:"name"`

    // ValueFrom specifies where the template content is loaded from.
    // +kubebuilder:validation:Required
    ValueFrom ValueSource `json:"valueFrom"`
}
```

Run `make -C go generate` after.

---

## Step 2: Template Resolution in Translator

**File:** `go/internal/controller/translator/agent/adk_api_translator.go`

Modify `resolveSystemMessage()` to apply Go templating when `promptTemplates` is non-empty:

```
resolveSystemMessage(ctx, agent)
  1. Get raw system message (existing logic — inline or ValueSource)
  2. If agent.Spec.Declarative.PromptTemplates is empty → return raw string (backwards compat)
  3. Fetch all referenced template contents via ValueSource.Resolve()
  4. Build template context:
     - .AgentName        = agent.Name
     - .AgentNamespace   = agent.Namespace
     - .Description      = agent.Spec.Description
     - .ToolNames        = collected from agent.Spec.Declarative.Tools[*].McpServer.ToolNames
     - .SkillNames       = collected from agent.Spec.Skills.Refs + agent.Spec.Skills.GitRefs[*].Name
  5. Register custom "include" function that looks up from resolved templates map
  6. Parse and execute template
  7. Return resolved string (or error)
```

New helper types/functions to add (can be in a new file like `template.go` in the same package):

```go
type PromptTemplateContext struct {
    AgentName      string
    AgentNamespace string
    Description    string
    ToolNames      []string
    SkillNames     []string
}

func resolvePromptTemplates(ctx context.Context, kube client.Client, namespace string, refs []v1alpha2.PromptTemplateRef) (map[string]string, error)

func executeSystemMessageTemplate(rawMessage string, templates map[string]string, tplCtx PromptTemplateContext) (string, error)
```

---

## Step 3: ConfigMap Watch in Agent Controller

**File:** `go/internal/controller/agent_controller.go`

Add a ConfigMap watch following the existing pattern (ModelConfig, RemoteMCPServer, Service):

```go
// In SetupWithManager, add:
Watches(
    &corev1.ConfigMap{},
    handler.EnqueueRequestsFromMapFunc(r.findAgentsReferencingConfigMap),
    builder.WithPredicates(predicate.ResourceVersionChangedPredicate{}),
)
```

Implement `findAgentsReferencingConfigMap`:
- List all Agents in the ConfigMap's namespace
- For each Agent, check if any `promptTemplates[*].valueFrom` references this ConfigMap (type=ConfigMap, name matches)
- Also check if `systemMessageFrom` references this ConfigMap
- Return reconcile requests for matching agents

---

## Step 4: Status Condition for Template Errors

**File:** `go/api/v1alpha2/agent_types.go`

No new condition type needed. Template resolution errors will cause reconciliation to fail, which already sets `Accepted=False` with `Reason=ReconcileFailed` and the error message. The existing pattern handles this.

---

## Step 5: Helm Chart — Built-in Prompts ConfigMap

**File:** `helm/kagent/templates/builtin-prompts-configmap.yaml` (new)

```yaml
apiVersion: v1
kind: ConfigMap
metadata:
  name: kagent-builtin-prompts
  namespace: {{ .Release.Namespace }}
data:
  skills-usage: |
    # Skills Usage
    You have access to skills — pre-built capabilities loaded from external sources.
    Skills are available as files in your skills directory. ...

  tool-usage-best-practices: |
    # Tool Usage Best Practices
    When using tools, follow these guidelines: ...

  safety-guardrails: |
    # Safety Guardrails
    You must follow these safety guidelines: ...

  kubernetes-context: |
    # Kubernetes Context
    You are operating within a Kubernetes cluster. ...

  a2a-communication: |
    # Agent-to-Agent Communication
    You can communicate with other agents. ...
```

The actual prompt content will be written based on real agent usage patterns. These are placeholders for the structure.

---

## Step 6: Example Usage

After implementation, an Agent YAML would look like:

```yaml
apiVersion: kagent.dev/v1alpha2
kind: Agent
metadata:
  name: my-agent
  namespace: default
spec:
  description: "A Kubernetes troubleshooting agent"
  declarative:
    modelConfig: default-model-config
    systemMessage: |
      {{include "skills-usage"}}

      You are {{.AgentName}}, a specialized agent for {{.Description}}.

      You have the following tools available: {{range .ToolNames}}{{.}}, {{end}}

      {{include "safety-guardrails"}}
    promptTemplates:
      - name: skills-usage
        valueFrom:
          type: ConfigMap
          name: kagent-builtin-prompts
          key: skills-usage
      - name: safety-guardrails
        valueFrom:
          type: ConfigMap
          name: kagent-builtin-prompts
          key: safety-guardrails
    tools:
      - type: McpServer
        mcpServer:
          name: my-tool-server
          kind: RemoteMCPServer
          apiGroup: kagent.dev
          toolNames: ["get-pods", "describe-pod"]
```

---

## Step 7: Tests

### Unit Tests

**File:** `go/internal/controller/translator/agent/template_test.go` (new)

- Test `executeSystemMessageTemplate` with include directives
- Test variable interpolation (.AgentName, .ToolNames, etc.)
- Test backwards compatibility: no `promptTemplates` → raw string passthrough
- Test error cases: missing template name, invalid template syntax, missing ConfigMap
- Test that included content is NOT treated as a template (no nested includes)

### E2E Test

**File:** `go/test/e2e/` (add to existing test suite)

- Create a ConfigMap with prompt content
- Create an Agent referencing it via `promptTemplates`
- Verify the resolved system message in the agent's config Secret
- Update the ConfigMap content → verify re-reconciliation produces updated prompt

---

## Files Changed (Summary)

| File | Change |
|------|--------|
| `go/api/v1alpha2/agent_types.go` | Add `PromptTemplates` field and `PromptTemplateRef` type |
| `go/internal/controller/translator/agent/adk_api_translator.go` | Modify `resolveSystemMessage()` to support templating |
| `go/internal/controller/translator/agent/template.go` | New — template resolution logic |
| `go/internal/controller/translator/agent/template_test.go` | New — unit tests |
| `go/internal/controller/agent_controller.go` | Add ConfigMap watch + `findAgentsReferencingConfigMap` |
| `helm/kagent/templates/builtin-prompts-configmap.yaml` | New — built-in prompt templates |
| `go/test/e2e/` | E2E test for prompt templates |

---

## Non-Goals (Explicit)

- No MCP prompts protocol integration
- No nested includes (templates from ConfigMaps are plain text)
- No new CRD (PromptTemplate kind) — ConfigMaps only for now
- No runtime resolution in Python ADK — controller handles everything
- No UI changes needed (resolved prompt is transparent to runtime)
