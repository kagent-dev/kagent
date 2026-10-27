# Shared models and MCP servers

Agents can use ModelConfigs and RemoteMCPServers in other namespaces. A reference
contains `name` and optionally `namespace`; omitting or leaving namespace empty
uses the namespace of the reference owner. Names never contain `namespace/name`.

AgentTemplate owns its model and MCP references, including child templates.
Inline templates use the Agent namespace. Harness owns memory and summarizer
model references. Agent-to-Harness and Agent-to-AgentTemplate references remain
local, as do subagent template references.

## Target-controlled access

Both ModelConfig and RemoteMCPServer expose `spec.allowedNamespaces`:

| Policy | Permitted origins |
| --- | --- |
| Omitted, empty `from`, or `Same` | The target's namespace |
| `All` | Any readable source namespace |
| `Selector` | Source namespaces whose labels match the selector |

Selectors apply even to references within the target's own namespace. A missing
selector, missing namespace, or failed observation denies access. Authorization
happens before reporting the target's dependent configuration errors, so denial
messages do not disclose its Secret references.

The following complete resources share a model and MCP server with `team-a`.
The Agent assumes an existing managed Harness named `runner` in `team-a`.

```yaml
apiVersion: v1
kind: Namespace
metadata:
  name: shared-config
---
apiVersion: v1
kind: Namespace
metadata:
  name: team-a
  labels:
    platform.example.com/shared-tools: "true"
---
apiVersion: api.kagent.dev/v1alpha3
kind: ModelConfig
metadata:
  name: shared-model
  namespace: shared-config
spec:
  provider: OpenAI
  model: example-model
  apiKeySecret: model-auth
  apiKeySecretKey: token
  allowedNamespaces:
    from: All
---
apiVersion: api.kagent.dev/v1alpha3
kind: RemoteMCPServer
metadata:
  name: shared-tools
  namespace: shared-config
spec:
  description: Shared tools for authorized namespaces
  url: https://tools.example.com/mcp
  protocol: STREAMABLE_HTTP
  allowedNamespaces:
    from: Selector
    selector:
      matchLabels:
        platform.example.com/shared-tools: "true"
---
apiVersion: api.kagent.dev/v1alpha3
kind: AgentTemplate
metadata:
  name: assistant
  namespace: team-a
spec:
  modelConfig:
    name: shared-model
    namespace: shared-config
  systemPrompt: Help the user using the available tools.
  tools:
    - mcp:
        server:
          kind: RemoteMCPServer
          name: shared-tools
          namespace: shared-config
---
apiVersion: api.kagent.dev/v1alpha3
kind: Agent
metadata:
  name: assistant
  namespace: team-a
spec:
  harnessRef:
    name: runner
  template:
    ref:
      name: assistant
```

Provision `shared-config/model-auth` separately with key `token`. Credentials are
not copied into `team-a`. Model and MCP Secret/ConfigMap references always resolve
in the target resource's namespace. Different credentials for the same HTTP origin
and header still conflict; namespacing does not relax that restriction.

## Controller visibility and namespace observation

Both source and target must be in the controller's configured `watchNamespaces`
and readable through its existing Kubernetes RBAC. A reference cannot widen the
watch scope. An invisible target produces a normal resolution error.

`Same` and `All` do not read Namespace metadata. `Selector` starts a field-selected
list/watch for the source namespace. Administrators may grant `list` and `watch`
on `namespaces` with `resourceNames: [team-a]` in a ClusterRole bound to the
controller. Blanket Namespace-list permission is unnecessary. The controller
never adds this grant itself. Observation failures invalidate cached labels and
appear in the consuming Agent's existing reference-resolution condition; other
agents continue processing. Watches retry to recover after access is restored.

Target updates, policy changes, deletion/recreation, namespace-label changes,
and target-local configuration dependencies trigger tracked recompilation.
Policy denial or a namespace observation failure prevents a new configuration
from compiling, just like other reference-resolution errors. The last successful
revision remains available for new sessions until a replacement is prepared.
Existing sessions retain their pinned revision. These policies constrain
compilation; they do not continuously authorize or revoke prepared revisions.

## Runtime credential access and management authorization

Reference permission authorizes use of the target's configured credentials, but
Substrate's credential provider independently controls which Secret namespaces
an actor's atespace can access. For the example, administrators configure the
Substrate release values explicitly:

```yaml
credentialProvider:
  namespacePolicies:
    - atespace: team-a
      allowedNamespaces: [team-a, shared-config]
```

For an embedded release, nest this under `substrate:`. The credential provider
also needs Kubernetes RBAC to read the Secret. Without either permission,
credential retrieval fails even if compilation succeeds. Kagent does not widen
these grants automatically. See [credential injection](credential-injection.md)
for destination-scoped header injection and Secret rotation behavior.

`All` grants reference use, not management visibility or direct tool invocation.
List/get/update APIs and direct MCP inspection retain their existing management
authorization. UI selection displays namespace-qualified shared resources and
preserves namespaces when saving references.

## Rollout

Install the regenerated CRDs **before** upgrading the controller or applying
namespace-qualified manifests. Older schemas can prune the namespace fields.
Existing local manifests retain their JSON shape. Go callers must adopt
`v1alpha3.ObjectReference` and `corev1.TypedObjectReference` for the changed fields.
