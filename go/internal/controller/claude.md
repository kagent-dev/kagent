# Kubernetes Controller

## Overview

The controller package implements the Kubernetes controllers for Kagent custom resources. It watches and reconciles CRDs to manage the lifecycle of agents, model configurations, and MCP servers within the Kubernetes cluster.

## Architecture

The controller follows the Kubernetes operator pattern using the controller-runtime framework. It implements reconciliation loops for:

- **Agent**: Main agent lifecycle management
- **ModelConfig**: LLM provider configuration
- **RemoteMCPServer**: Remote Model Context Protocol server management
- **MCPServerTool**: Legacy tool server management (being phased out)
- **Service**: Kubernetes service integration

## Key Components

### Controllers

#### AgentController (`agent_controller.go`)
- **Purpose**: Reconciles Agent custom resources
- **Watches**:
  - Primary: `v1alpha2.Agent` resources
  - Secondary: `v1alpha2.ModelConfig` (triggers reconciliation when model config changes)
  - Secondary: `v1alpha1.MCPServer` (KMCP integration)
  - Owned: Resources created by the ADK translator (Deployments, Services, ConfigMaps, etc.)
- **Key Logic**: Delegates to `KagentReconciler.ReconcileKagentAgent()`
- **Features**:
  - Leader election enabled
  - Generation-based change detection
  - Automatic re-reconciliation when ModelConfig or MCPServer changes
  - Manages ownership of agent-related Kubernetes resources

#### ModelConfigController (`modelconfig_controller.go`)
- **Purpose**: Reconciles ModelConfig custom resources
- **Watches**:
  - Primary: `v1alpha2.ModelConfig` resources
  - Secondary: Secrets (triggers reconciliation when referenced secrets change)
- **Key Logic**: Delegates to `KagentReconciler.ReconcileKagentModelConfig()`
- **Features**:
  - Watches for secret changes and triggers dependent agents
  - Validates LLM provider configurations

#### RemoteMCPServerController (`remote_mcp_server_controller.go`)
- **Purpose**: Reconciles RemoteMCPServer custom resources
- **Watches**: `v1alpha2.RemoteMCPServer` resources
- **Key Logic**: Delegates to `KagentReconciler.ReconcileKagentRemoteMCPServer()`
- **Features**:
  - Periodic reconciliation (60-second requeue) for status refresh
  - Manages remote MCP server connections and tool availability

#### MCPServerToolController (`mcp_server_tool_controller.go`)
- **Purpose**: Legacy tool server management (being phased out in favor of KMCP)
- **Note**: This controller is deprecated as the project transitions to first-class KMCP support

#### ServiceController (`service_controller.go`)
- **Purpose**: Manages Kubernetes Services for agents
- **Key Logic**: Handles service creation and updates for agent endpoints

### Reconciler Pattern

The controllers delegate actual reconciliation logic to the `reconciler.KagentReconciler` interface, which is implemented in the `go/internal/controller/reconciler/` package. This separation allows for cleaner testing and modular reconciliation logic.

## Dependencies

### External Dependencies
- `sigs.k8s.io/controller-runtime`: Core Kubernetes controller framework
- `k8s.io/apimachinery`: Kubernetes API machinery
- `k8s.io/api`: Standard Kubernetes API types

### Internal Dependencies
- `github.com/kagent-dev/kagent/go/api/v1alpha2`: Kagent CRD definitions
- `github.com/kagent-dev/kmcp/api/v1alpha1`: KMCP CRD definitions
- `github.com/kagent-dev/kagent/go/internal/controller/reconciler`: Reconciliation logic
- `github.com/kagent-dev/kagent/go/internal/controller/translator/agent`: API translation layer

## RBAC Permissions

Controllers require specific Kubernetes RBAC permissions (defined via kubebuilder annotations):

- **Agents**: Full CRUD on agents, agents/status, agents/finalizers
- **ModelConfigs**: Full CRUD on modelconfigs, modelconfigs/status
- **RemoteMCPServers**: Full CRUD on remotemcpservers, remotemcpservers/status
- **Core Resources**: CRUD on Secrets, ServiceAccounts, ConfigMaps
- **Deployments**: CRUD on apps/deployments

## Workflow

### Agent Reconciliation Flow

1. **Watch**: Controller watches for Agent CRD changes (create, update, delete)
2. **Reconcile Trigger**: Change detected or periodic resync
3. **Delegate**: Call `KagentReconciler.ReconcileKagentAgent()`
4. **Resource Creation**: Reconciler creates/updates Kubernetes resources:
   - Deployment (for agent pod)
   - Service (for agent endpoint)
   - ConfigMaps (for configuration)
   - ServiceAccount (for RBAC)
5. **Status Update**: Update Agent status with current state
6. **Requeue**: Return result with requeue timing if needed

### Change Detection

Controllers use predicates to optimize reconciliation:
- `GenerationChangedPredicate`: Only reconcile on spec changes
- `LabelChangedPredicate`: Reconcile on label changes
- `ResourceVersionChangedPredicate`: Reconcile on resource updates

### Secondary Resource Watching

Controllers watch secondary resources and trigger reconciliation of dependent resources:
- ModelConfig changes → reconcile all Agents using that ModelConfig
- Secret changes → reconcile all ModelConfigs using that Secret
- MCPServer changes → reconcile all Agents using that MCPServer

## Configuration

Controllers are configured and registered in the main controller manager setup (typically in `main.go` or `cmd/controller/`). Each controller receives:
- Kubernetes Scheme
- Reconciler implementation
- Translator (for Agent controller)

## Error Handling

- Errors during reconciliation are logged and returned
- The controller-runtime framework automatically requeues failed reconciliations with exponential backoff
- Controllers can explicitly requeue with custom timing via `ctrl.Result{RequeueAfter: duration}`

## Testing

Controllers can be tested using:
- **Unit Tests**: Mock reconciler interface
- **Integration Tests**: Use envtest (Kubernetes API server for testing)
- **E2E Tests**: Deploy to real/kind cluster

## Related Components

- **Reconciler** (`go/internal/controller/reconciler/`): Implements actual reconciliation logic
- **Translator** (`go/internal/controller/translator/`): Translates CRD specs to Kubernetes resources
- **CRD Definitions** (`go/api/v1alpha2/`): Custom resource type definitions
- **HTTP Server** (`go/internal/httpserver/`): REST API for agent management

## Key Files

- `agent_controller.go`: Agent lifecycle management (lines 62-64 for main reconciliation)
- `modelconfig_controller.go`: Model configuration management
- `remote_mcp_server_controller.go`: Remote MCP server management (60s periodic reconciliation at line 46-49)
- `mcp_server_tool_controller.go`: Legacy tool server support
- `service_controller.go`: Kubernetes service management

## Future Enhancements

- **KMCP Integration**: Transitioning from legacy tool servers to first-class KMCP support
- **Multi-cluster**: Support for agents spanning multiple Kubernetes clusters
- **Advanced Scheduling**: Custom scheduling logic for agent placement
