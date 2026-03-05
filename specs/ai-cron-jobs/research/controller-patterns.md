# Controller Patterns

## Existing Controllers (6 total)
All use shared reconciler pattern via `KagentReconciler` interface.

| Controller | CRD | DB Interaction |
|------------|-----|----------------|
| AgentController | Agent | StoreAgent() |
| ModelConfigController | ModelConfig | None |
| ModelProviderConfigController | ModelProviderConfig | None |
| RemoteMCPServerController | RemoteMCPServer | StoreToolServer(), RefreshToolsForServer() |
| ServiceController | corev1.Service | StoreToolServer(), RefreshToolsForServer() |
| MCPServerToolController | MCPServer (kmcp) | StoreToolServer(), RefreshToolsForServer() |

## Registration Pattern (go/pkg/app/app.go)
```go
rcnclr := reconciler.NewKagentReconciler(apiTranslator, client, dbClient, ...)

if err := (&controller.YourController{
    Scheme:     mgr.GetScheme(),
    Reconciler: rcnclr,
}).SetupWithManager(mgr); err != nil { ... }
```

## RBAC Markers
```go
// +kubebuilder:rbac:groups=kagent.dev,resources=agentcronjobs,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=kagent.dev,resources=agentcronjobs/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=kagent.dev,resources=agentcronjobs/finalizers,verbs=update
```

## Note for AgentCronJob
Unlike other controllers, AgentCronJob needs a **timer/scheduler** mechanism — not just event-driven reconciliation. Options:
- RequeueAfter with calculated next-run duration
- In-memory cron scheduler (e.g., robfig/cron)
- Periodic reconciliation checking schedule vs current time
