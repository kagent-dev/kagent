# agentgateway integration

This optional Go module connects agentgateway resources to native kagent consumers.
It is registered explicitly on an existing controller-runtime manager, including
through app.Options.SetupWithManager. The core controller does not import it.

Resources annotated with kagent.dev/connection=true produce:

| Source | Derived resources |
| --- | --- |
| AgentgatewayModel | ModelConfig |
| MCP AgentgatewayBackend | RemoteMCPServer, HTTPRoute, optional AgentgatewayPolicy |

Config supplies the Gateway reference and model/MCP origins. Native model and MCP
template functions preserve application settings, discovery authentication and
credential references. MCPPolicy accepts an agentgateway traffic policy; the caller
owns its identity and authorization configuration. Consumer templates supply
public model/connection settings and Secret references.

The component sets owner references, refuses to adopt existing unowned consumers,
watches credential changes, and reports KagentReady. Model readiness appears in
its own status.parents entry with controllerName kagent.dev/agentgateway-connections.
Removing the selection annotation removes owned consumers and readiness; Kubernetes
garbage collection handles source deletion.

Install kagent, agentgateway and Gateway API CRDs before registration. The host
manager controls watched namespaces and leader election. Its ServiceAccount needs
get/list/watch on gateway sources and Secrets; create/update/patch/delete on native
consumers and routes/policies; and update/patch on source status.

Register it in a controller host after installing the CRDs:

```go
import (
    "github.com/kagent-dev/kagent/go/integrations/agentgateway"
    ctrl "sigs.k8s.io/controller-runtime"
    gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"
)

func setup(manager ctrl.Manager) error {
    return agentgateway.SetupWithManager(manager, agentgateway.Config{
        Gateway: gatewayv1.ParentReference{Name: "agentgateway"},
        ModelURL: "http://agentgateway.example.com:8080",
        MCPURL: "http://agentgateway.example.com:8080",
    })
}
```

Run tests against the Kubernetes test API with the existing envtest assets:

    KUBEBUILDER_ASSETS=/path/to/envtest go test -race ./...

Tests load the upstream CRDs from the pinned Go module sources.
