# Agent manifests

Read this file when you choose values for an Agent, or write, check, fix, or recover a manifest.
`kagent apply` rejects a field that is not in these shapes, with `unknown field`.

## Choosing values

Pick one value for each line of the card. Do not list alternatives: the user can change any line.

| Value | Default | Note |
| --- | --- | --- |
| Harness | Runtime `kagent`. It supports all model providers. | `claude`: Anthropic, Bedrock, AnthropicVertexAI only, and it binds whole MCP servers (see [Agent manifest](#agent-manifest)). `codex`: OpenAI with `openAI.apiFormat: responses`, or Bedrock. |
| Model | `default-model-config`, else the first in `model list` | |
| Template | A new inline template | Reuse a template from `agent template list` that fits the goal. |
| Name | Lowercase letters, digits, and hyphens from the goal | Not in `agent list`, or `agent template list` for a template. `apply` replaces a resource with the same name. |
| Tools | Only the tools that the goal needs, by exact name | Mark a tool that changes state "(needs approval)", and a helper "(helper)". |

## Find tools

```sh
kagent tool list -o json | jq -r '.toolServers[]? | .ref as $s | .discoveredTools[]? | "\($s) \(.name): \(.description)"' | grep ' k8s_'
```

- Each line starts with `NAMESPACE/SERVER`. Filter on a tool prefix, such as `k8s_` or `helm_`.
- Check each name that you choose against this list. `apply` does not check tool names.

## Readiness check

```sh
kagent agent get NAME -o json | jq '{generation: .metadata.generation, observed: .status.observedGeneration, revision: .status.desiredRevision, warnings: .status.warnings, conditions: [.status.conditions[]? | {type, status, reason, message}]}'
```

Show each entry of `warnings` to the user, also when the Agent is ready.

| Result (the first row that matches) | Meaning and next step |
| --- | --- |
| `ResolvedRefs` or `Compatible` is `False` | Failure. Stop. Find the `message` in [Failures](#failures). |
| `observed` is less than `generation`, or there is no `Ready` condition | The controller did not report yet. Run the check again. |
| `Ready` is `False` with reason `ActorTemplatePending` | The runtime is in preparation. Run the check again. |
| `observed` equals `generation`, and `Ready` is `True` | The Agent is ready. |

## Agent manifest

Replace each upper-case word. Remove a line that you do not need.

```yaml
apiVersion: api.kagent.dev/v1alpha3
kind: Agent
metadata:
  name: NAME                       # lowercase-hyphens, not in `agent list`
spec:
  harnessRef:
    name: HARNESS                  # kagent harness list
  template:
    inline:                        # or, to reuse one: ref: {name: TEMPLATE}
      description: PURPOSE         # one line
      modelConfig:
        name: MODEL                # kagent model list
      systemPrompt: |              # see "Write the prompt"
        PROMPT
      tools:
        - mcp:
            server:
              name: SERVER         # kagent tool list, the part after NS/
            tools: [TOOL, TOOL]
            requireApproval: true  # only on a binding with tools that change state
```

- Do not set `metadata.namespace`. The CLI uses `-n`, and a different value fails.
- Put one resource in each file. `apply` reads only the first YAML document.
- Each `tools` item binds one server. Use two items on one server to split read tools from tools that need approval.
  If you omit the inner `tools` list, the agent gets every tool of the server, also the tools that write.
- A tool changes state when its name or description says that it creates, changes, deletes, or runs something,
  for example `k8s_check_service_connectivity`, which starts a temporary pod.
- `requireApproval: true` pauses each call until the user approves it in the kagent dashboard.
- On the `claude` harness, a `tools` list binds the whole MCP server, and the Agent reports a warning. The CRD says:
  "Harnesses that cannot enforce a partial selection may expose the whole server and report a warning."
  So the split above does not work there. Set `requireApproval: true` on each binding to a server that has
  tools that change state, and show `SERVER (all tools)` on the card.
- Some read tools can show Secret data, for example `k8s_get_resource_yaml`. Bind them only when the goal needs them.

## Sandbox tools

Bind these from server `kagent-api` when the agent needs an isolated workspace.

| Tools | State |
| --- | --- |
| `list_sandbox_templates`, `list_sandboxes`, `get_sandbox`, `get_sandbox_process`, `read_sandbox_outputs`, `read_sandbox_file` | Read only |
| `create_sandbox`, `write_sandbox_file`, `start_sandbox_process`, `kill_sandbox_process`, `suspend_sandbox`, `resume_sandbox`, `delete_sandbox` | Change only the sandbox |

- A usual set is `list_sandbox_templates`, `create_sandbox`, `get_sandbox`, `write_sandbox_file`, `start_sandbox_process`,
  `get_sandbox_process`, `read_sandbox_outputs`, `read_sandbox_file`, and `delete_sandbox`.
- Put them in one binding. Ask once whether `start_sandbox_process` needs approval.
- Do not bind the `shell` tool of `kagent-tool-server` for a workspace. It runs in the shared tool-server pod.
- `kagent sandbox templates` must show a template in `-n`. If it is empty, an admin creates a SandboxTemplate first.
- The agent's sandbox calls use the identity of its connection to `kagent-api`. To act as the invoking user, an admin
  sets the Helm value `config.runtimeDefaults.tokenPropagation.enabled=true`. It applies to the `kagent` runtime only.

## Reuse a template

An AgentTemplate `spec` is the `inline` block above, with `kind: AgentTemplate`. Use one when two or more
Agents share one behavior. Apply it first, in its own file. It has no status, so run the readiness check
on each Agent that refers to it:

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

## Add a model

Use this when `model list` is empty or no model fits the harness. `kagent apply` does not take a ModelConfig,
so the user applies it with `kubectl`. This example is for Anthropic, which the `kagent` and `claude` harnesses support.

```yaml
apiVersion: api.kagent.dev/v1alpha3
kind: ModelConfig
metadata: {name: MODEL}
spec:
  provider: Anthropic
  model: MODEL_ID                 # an Anthropic model ID
  apiKeySecret: SECRET
  apiKeySecretKey: ANTHROPIC_API_KEY
```

Give the user these commands to run in their own terminal. Never ask for the API key in chat:

```sh
read -rs KEY && kubectl create secret generic SECRET -n NAMESPACE --from-literal=ANTHROPIC_API_KEY="$KEY"; unset KEY
kubectl apply -n NAMESPACE -f MODEL.yaml
```

Then run `kagent model list` and continue with Confirm.

## Write the prompt

- State the role, the steps, the output form, and the limits.
- Get a per-task value, such as a namespace, from the task. Tell the agent to ask when it is missing.
- If a tool description gives a default scope, put it in the prompt. For example,
  `k8s_get_resources` reads only its own namespace unless the call sets one.

```text
You explain why pods in one Kubernetes namespace fail. Get the namespace from the task, or ask for it.
Set the namespace on each tool call. List the pods. Read the events and logs of each failing pod.
Give one line for each failing pod: the pod, the cause, and the fix.
Do not change any resource.
```

## Failures

`apply` creates or changes the Agent also when a condition then fails. Fix the file and apply
again, or delete the Agent. Match the `apply` error, or the `message` of the failed condition, to the first row that fits.

| Text | Fix |
| --- | --- |
| `unknown field "FIELD"` | Correct the field name with the shapes in this file. |
| `internal server error` | Remove all `metadata` fields except `name`. |
| `resolve Harness "NS/NAME": not found` | Use a NAME from `kagent harness list`. If the list is empty, see "Troubleshooting" in SKILL.md. |
| `resolve ModelConfig "NS/NAME": secret NAME not found` | An admin must create the API key Secret of the ModelConfig. Or use another model. |
| `resolve ModelConfig "NS/NAME": not found` | Use a NAME from `kagent model list`. |
| `resolve RemoteMCPServer "NAME": not found` | Use a SERVER from `kagent tool list`. |
| `resolve AgentTemplate "NS/NAME": not found` (a `ref`), or `"NAME"` (a `subAgent`) | Apply the AgentTemplate first, or use a NAME from `kagent agent template list`. |
| `ModelConfig is required` | Add `modelConfig.name` to the template. |
| `does not support ModelConfig provider` | Use the harness with runtime `kagent`, or a model that the harness supports. |
| `Codex OpenAI requires openAI.apiFormat responses` | Use the harness with runtime `kagent`, or a ModelConfig with `openAI.apiFormat: responses`. |
| `denied by allowedNamespaces` | Use a model or tool server from `-n`. Or an admin adds this namespace to its `allowedNamespaces`. For the built-in tool server, the Helm value is `kagent-tools.remoteMCPServer.allowedNamespaces`. |

This command shows the API key Secret of each ModelConfig, for example after a `401 Unauthorized`:

```sh
kagent model list -o json | jq -r '.modelConfigs[]? | "\(.ref.name) \(.resource.value.spec.apiKeySecret) \(.resource.value.spec.apiKeySecretKey)"'
```

## Get a manifest back

If you do not have the file of an Agent, write it again from the server:

```sh
kagent agent get NAME -o json | jq '{apiVersion, kind, metadata: {name: .metadata.name}, spec}' > NAME.yaml
```

JSON is valid YAML, so Update can apply this file. Server fields such as `resourceVersion` make `apply` fail. Labels are fine.
`kagent agent template get NAME -o json` works the same way.
