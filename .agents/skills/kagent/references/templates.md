# Templates and helpers

Read this file when Agents share a behavior, an agent hands work to a helper, or before you delete an AgentTemplate.

## Reuse a template

An AgentTemplate `spec` is the `inline` block of the [Agent manifest](agent-reference.md#agent-manifest),
with `kind: AgentTemplate`. Use one when two or more Agents share one behavior. Apply it first, in its own file.
It has no status, so run the [Readiness check](agent-reference.md#readiness-check) on each Agent that refers to it:

```yaml
apiVersion: api.kagent.dev/v1alpha3
kind: Agent
metadata: {name: NAME}
spec:
  harnessRef: {name: HARNESS}
  template: {ref: {name: TEMPLATE}}  # kagent agent template list
```

## Helper agents

A `subAgent` binding lets an agent hand work to an AgentTemplate in the same namespace. The helper runs in the
agent's own runtime, on its Harness. On `codex` and `claude`, the helper must use the same model provider and key. On `codex`, it cannot have tools.
Put the helper AgentTemplate in its own file, and apply it first:

```yaml
apiVersion: api.kagent.dev/v1alpha3
kind: AgentTemplate
metadata: {name: TEMPLATE}
spec:
  description: PURPOSE
  modelConfig: {name: MODEL}
  systemPrompt: PROMPT
```

The helper is not an Agent. It does not show in `agent list` and has no Session.
To also chat with it, create an Agent that refers to the template. Then bind it in the agent:

```yaml
apiVersion: api.kagent.dev/v1alpha3
kind: Agent
metadata: {name: NAME}
spec:
  harnessRef: {name: HARNESS}
  template:
    inline:
      description: PURPOSE
      modelConfig: {name: MODEL}
      systemPrompt: PROMPT
      tools:
        - subAgent:
            name: HELPER                    # the tool name that the agent sees
            description: WHEN_TO_USE        # tells the agent when to hand work to the helper
            templateRef: {name: TEMPLATE}   # kagent agent template list
```

## Find what uses a template

Before you delete an AgentTemplate, run both lists. Each prints the resources that refer to `TEMPLATE`:

```sh
kagent agent list -o json | jq -r --arg t TEMPLATE '.items[]? | select([.spec.template.ref.name, (.. | .subAgent?.templateRef?.name?)] | index($t)) | .metadata.name'
kagent agent template list -o json | jq -r --arg t TEMPLATE '.items[]? | select([.. | .subAgent?.templateRef?.name?] | index($t)) | .metadata.name'
```
