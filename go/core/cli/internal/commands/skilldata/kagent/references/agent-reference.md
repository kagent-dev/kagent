# Agent manifests

## Choosing values

Pick one value for each field. Do not list alternatives: the user can change any value.

| Value | Default | Note |
| --- | --- | --- |
| Harness | Runtime `kagent`. It supports all model providers. | Check the [Compatibility](#compatibility) with the model. |
| Model | `default-model-config`, else the first in `model list` | |
| Template | A new inline template | Reuse a template from `agent template list` that fits the request. See [templates.md](templates.md). |
| Name | Lowercase letters, digits, and hyphens from the request | Not in `agent list`, or `agent template list` for a template. `apply` replaces a resource with the same name. |
| Tools | Only the tools that the request needs, by exact name | |

## Compatibility

No command lists the providers that a runtime other than `kagent` supports. For those runtimes, the `Compatible`
message of the Readiness check after Apply names what does not fit. Find the fix in [Failures](#failures).

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
| `observed` is less than `generation`, or `Ready` is not `True` | Not ready yet. Run the check again. After 5 minutes, stop and report the `reason` and `message`. For `ActorTemplatePending`, an admin checks the WorkerPool. |
| `observed` equals `generation`, and `Ready` is `True` | The Agent is ready. |

## Agent manifest

Remove a line that you do not need.

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
              kind: RemoteMCPServer  # kagent 1.0.x requires it
              name: SERVER         # kagent tool list, the part after NS/
            tools: [TOOL, TOOL]
            requireApproval: true  # only on a binding with tools that change state
```

- Do not set `metadata.namespace`. The CLI uses `-n`, and a different value fails.
- Put one resource in each file. `apply` reads only the first YAML document.
- Each `tools` item binds one server. To split read tools from tools that need approval, use two items on one server.
  Some runtimes reject that: see [Failures](#failures). If you omit the inner `tools` list, the agent gets every tool
  of the server, also the tools that write.
- A tool changes state when its name or description says that it creates, changes, deletes, or runs something,
  for example `k8s_check_service_connectivity`, which starts a temporary pod.
- If the Readiness check shows a warning with `exposing the whole server`, the runtime ignored the `tools` list.
  If that server has tools that change state, set `requireApproval: true` on its binding. Tell the user that the agent
  gets all tools of that server.
- Some read tools can show Secret data, for example `k8s_get_resource_yaml`. Bind them only when the request needs them.

## Add a model

Use this when `model list` is empty or no model fits the harness. `kagent apply` does not take a ModelConfig,
so the user applies it with `kubectl`. This example is for Anthropic.

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

Then run `kagent model list` and continue with Step 2 of Create.

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
| `internal server error` | Remove all `metadata` fields except `name`. |
| `resolve ModelConfig "NS/NAME": secret NAME not found` | An admin must create the API key Secret of the ModelConfig. Or use another model. |
| `resolve KIND "...": not found` | Use a name from the list for that kind: `kagent harness list`, `model list`, `tool list`, or `agent template list`. Apply a missing AgentTemplate first. If `harness list` is empty, see "Troubleshooting" in SKILL.md. |
| `map to the same native name` | Merge the bindings of that server into one `tools` item. If one of its tools changes state, set `requireApproval: true` on it. |
| `must use the root` ... `provider and authentication configuration` | Give the helper template the same `modelConfig` as the agent. |
| `cannot contain` (a helper) | Remove the listed items, such as `tools`, from the helper template. |
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

JSON is valid YAML, so `apply` takes this file.
`kagent agent template get NAME -o json` works the same way.
