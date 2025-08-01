package reconciler

import (
	"bytes"
	"context"
	"crypto/sha256"
	"fmt"
	"reflect"
	"sync"

	"github.com/hashicorp/go-multierror"
	appsv1 "k8s.io/api/apps/v1"
	k8s_errors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"

	"github.com/kagent-dev/kagent/go/controller/api/v1alpha1"
	"github.com/kagent-dev/kagent/go/controller/internal/a2a"
	"github.com/kagent-dev/kagent/go/controller/translator"
	"github.com/kagent-dev/kagent/go/internal/adk"
	"github.com/kagent-dev/kagent/go/internal/database"
	"github.com/kagent-dev/kagent/go/internal/utils"
	"github.com/kagent-dev/kagent/go/internal/version"
	mcp_client "github.com/mark3labs/mcp-go/client"
	"github.com/mark3labs/mcp-go/client/transport"
	"github.com/mark3labs/mcp-go/mcp"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

var (
	reconcileLog = ctrl.Log.WithName("reconciler")
)

type KagentReconciler interface {
	ReconcileKagentAgent(ctx context.Context, req ctrl.Request) error
	ReconcileKagentModelConfig(ctx context.Context, req ctrl.Request) error
	ReconcileKagentApiKeySecret(ctx context.Context, req ctrl.Request) error
	ReconcileKagentToolServer(ctx context.Context, req ctrl.Request) error
	ReconcileKagentMemory(ctx context.Context, req ctrl.Request) error
}

type kagentReconciler struct {
	adkTranslator translator.AdkApiTranslator
	a2aReconciler a2a.A2AReconciler

	kube     client.Client
	dbClient database.Client

	defaultModelConfig types.NamespacedName

	// TODO: Remove this lock since we have a DB which we can batch anyway
	upsertLock sync.Mutex
}

func NewKagentReconciler(
	translator translator.AdkApiTranslator,
	kube client.Client,
	dbClient database.Client,
	defaultModelConfig types.NamespacedName,
	a2aReconciler a2a.A2AReconciler,
) KagentReconciler {
	return &kagentReconciler{
		adkTranslator:      translator,
		kube:               kube,
		dbClient:           dbClient,
		defaultModelConfig: defaultModelConfig,
		a2aReconciler:      a2aReconciler,
	}
}

func (a *kagentReconciler) ReconcileKagentAgent(ctx context.Context, req ctrl.Request) error {
	// TODO(sbx0r): missing finalizer logic

	agent := &v1alpha1.Agent{}
	if err := a.kube.Get(ctx, req.NamespacedName, agent); err != nil {
		if k8s_errors.IsNotFound(err) {
			return a.handleAgentDeletion(req)
		}

		return fmt.Errorf("failed to get agent %s/%s: %w", req.Namespace, req.Name, err)
	}

	return a.handleExistingAgent(ctx, agent, req)
}

func (a *kagentReconciler) handleAgentDeletion(req ctrl.Request) error {
	// TODO(sbx0r): handle deletion of agents with multiple teams assignment

	// agents, err := a.findTeamsUsingAgent(ctx, req)
	// if err != nil {
	// 	return fmt.Errorf("failed to find teams for agent %s/%s: %v", req.Namespace, req.Name, err)
	// }
	// if len(agents) > 1 {
	// 	reconcileLog.Info("agent with multiple dependencies was deleted",
	// 	"namespace", req.Namespace,
	// 	"name", req.Name,
	// 	"agents", agents)
	// }

	// remove a2a handler if it exists
	a.a2aReconciler.ReconcileAgentDeletion(req.NamespacedName.String())

	if err := a.dbClient.DeleteAgent(req.NamespacedName.String()); err != nil {
		return fmt.Errorf("failed to delete agent %s: %w",
			req.NamespacedName.String(), err)
	}

	reconcileLog.Info("Agent was deleted", "namespace", req.Namespace, "name", req.Name)
	return nil
}

func (a *kagentReconciler) handleExistingAgent(ctx context.Context, agent *v1alpha1.Agent, req ctrl.Request) error {
	reconcileLog.Info("Agent Event",
		"namespace", req.Namespace,
		"name", req.Name,
		"oldGeneration", agent.Status.ObservedGeneration,
		"newGeneration", agent.Generation)

	return a.reconcileAgents(ctx, agent)
}

func (a *kagentReconciler) reconcileAgentStatus(ctx context.Context, agent *v1alpha1.Agent, configHash *[sha256.Size]byte, inputErr error) error {
	var (
		status  metav1.ConditionStatus
		message string
		reason  string
	)
	if inputErr != nil {
		status = metav1.ConditionFalse
		message = inputErr.Error()
		reason = "AgentReconcileFailed"
		reconcileLog.Error(inputErr, "failed to reconcile agent", "agent", utils.GetObjectRef(agent))
	} else {
		status = metav1.ConditionTrue
		reason = "AgentReconciled"
	}

	conditionChanged := meta.SetStatusCondition(&agent.Status.Conditions, metav1.Condition{
		Type:               v1alpha1.AgentConditionTypeAccepted,
		Status:             status,
		LastTransitionTime: metav1.Now(),
		Reason:             reason,
		Message:            message,
	})

	deployedCondition := metav1.Condition{
		Type:               v1alpha1.AgentConditionTypeReady,
		Status:             metav1.ConditionUnknown,
		LastTransitionTime: metav1.Now(),
	}

	// Check if the deployment exists
	deployment := &appsv1.Deployment{}
	if err := a.kube.Get(ctx, types.NamespacedName{Namespace: agent.Namespace, Name: agent.Name}, deployment); err != nil {
		deployedCondition.Status = metav1.ConditionUnknown
		deployedCondition.Reason = "DeploymentNotFound"
		deployedCondition.Message = err.Error()
	} else {
		replicas := int32(1)
		if deployment.Spec.Replicas != nil {
			replicas = *deployment.Spec.Replicas
		}
		if deployment.Status.AvailableReplicas == replicas {
			deployedCondition.Status = metav1.ConditionTrue
			deployedCondition.Reason = "DeploymentReady"
			deployedCondition.Message = "Deployment is ready"
		} else {
			deployedCondition.Status = metav1.ConditionFalse
			deployedCondition.Reason = "DeploymentNotReady"
			deployedCondition.Message = fmt.Sprintf("Deployment is not ready, %d/%d pods are ready", deployment.Status.AvailableReplicas, replicas)
		}
	}

	conditionChanged = meta.SetStatusCondition(&agent.Status.Conditions, deployedCondition)

	// Only update the config hash if the config hash has changed and there was no error
	configHashChanged := configHash != nil && !bytes.Equal((agent.Status.ConfigHash)[:], (*configHash)[:])

	// update the status if it has changed or the generation has changed
	if conditionChanged || agent.Status.ObservedGeneration != agent.Generation || configHashChanged {
		// If the config hash is nil, it means there was an error during the reconciliation
		if configHash != nil {
			agent.Status.ConfigHash = (*configHash)[:]
		}
		agent.Status.ObservedGeneration = agent.Generation
		if err := a.kube.Status().Update(ctx, agent); err != nil {
			return fmt.Errorf("failed to update agent status: %v", err)
		}
	}
	return nil
}

func (a *kagentReconciler) ReconcileKagentModelConfig(ctx context.Context, req ctrl.Request) error {
	modelConfig := &v1alpha1.ModelConfig{}
	if err := a.kube.Get(ctx, req.NamespacedName, modelConfig); err != nil {
		return fmt.Errorf("failed to get model %s: %v", req.Name, err)
	}

	agents, err := a.findAgentsUsingModel(ctx, req)
	if err != nil {
		return fmt.Errorf("failed to find agents for model %s: %v", req.Name, err)
	}

	return a.reconcileModelConfigStatus(
		ctx,
		modelConfig,
		a.reconcileAgents(ctx, agents...),
	)
}

func (a *kagentReconciler) reconcileModelConfigStatus(ctx context.Context, modelConfig *v1alpha1.ModelConfig, err error) error {
	var (
		status  metav1.ConditionStatus
		message string
		reason  string
	)
	if err != nil {
		status = metav1.ConditionFalse
		message = err.Error()
		reason = "ModelConfigReconcileFailed"
		reconcileLog.Error(err, "failed to reconcile model config", "modelConfig", utils.GetObjectRef(modelConfig))
	} else {
		status = metav1.ConditionTrue
		reason = "ModelConfigReconciled"
	}

	conditionChanged := meta.SetStatusCondition(&modelConfig.Status.Conditions, metav1.Condition{
		Type:               v1alpha1.ModelConfigConditionTypeAccepted,
		Status:             status,
		LastTransitionTime: metav1.Now(),
		Reason:             reason,
		Message:            message,
	})

	// update the status if it has changed or the generation has changed
	if conditionChanged || modelConfig.Status.ObservedGeneration != modelConfig.Generation {
		modelConfig.Status.ObservedGeneration = modelConfig.Generation
		if err := a.kube.Status().Update(ctx, modelConfig); err != nil {
			return fmt.Errorf("failed to update model config status: %v", err)
		}
	}
	return nil
}

func (a *kagentReconciler) ReconcileKagentApiKeySecret(ctx context.Context, req ctrl.Request) error {
	agents, err := a.findAgentsUsingApiKeySecret(ctx, req)
	if err != nil {
		return fmt.Errorf("failed to find agents for secret %s: %v", req.Name, err)
	}

	return a.reconcileAgents(ctx, agents...)
}

func (a *kagentReconciler) ReconcileKagentToolServer(ctx context.Context, req ctrl.Request) error {
	// reconcile the agent team itself
	toolServer := &v1alpha1.ToolServer{}
	if err := a.kube.Get(ctx, req.NamespacedName, toolServer); err != nil {
		// if the tool server is not found, we can ignore it
		if k8s_errors.IsNotFound(err) {
			return nil
		}
		return fmt.Errorf("failed to get tool server %s: %v", req.Name, err)
	}

	reconcileErr := a.reconcileToolServer(ctx, toolServer)

	// update the tool server status as the agents depend on it
	if err := a.reconcileToolServerStatus(
		ctx,
		toolServer,
		utils.GetObjectRef(toolServer),
		reconcileErr,
	); err != nil {
		return fmt.Errorf("failed to reconcile tool server %s: %v", req.Name, err)
	}

	// find and reconcile all agents which use this tool server
	agents, err := a.findAgentsUsingToolServer(ctx, req)
	if err != nil {
		return fmt.Errorf("failed to find teams for agent %s: %v", req.Name, err)
	}

	if err := a.reconcileAgents(ctx, agents...); err != nil {
		return fmt.Errorf("failed to reconcile agents for tool server %s, see status for more details", req.Name)
	}

	return nil
}

func (a *kagentReconciler) reconcileToolServerStatus(
	ctx context.Context,
	toolServer *v1alpha1.ToolServer,
	serverRef string,
	err error,
) error {
	discoveredTools, discoveryErr := a.getDiscoveredMCPTools(ctx, serverRef)
	if discoveryErr != nil {
		err = multierror.Append(err, discoveryErr)
	}

	var (
		status  metav1.ConditionStatus
		message string
		reason  string
	)
	if err != nil {
		status = metav1.ConditionFalse
		message = err.Error()
		reason = "AgentReconcileFailed"
		reconcileLog.Error(err, "failed to reconcile agent", "tool_server", utils.GetObjectRef(toolServer))
	} else {
		status = metav1.ConditionTrue
		reason = "AgentReconciled"
	}
	conditionChanged := meta.SetStatusCondition(&toolServer.Status.Conditions, metav1.Condition{
		Type:               v1alpha1.AgentConditionTypeAccepted,
		Status:             status,
		LastTransitionTime: metav1.Now(),
		Reason:             reason,
		Message:            message,
	})

	// only update if the status has changed to prevent looping the reconciler
	if !conditionChanged &&
		toolServer.Status.ObservedGeneration == toolServer.Generation &&
		reflect.DeepEqual(toolServer.Status.DiscoveredTools, discoveredTools) {
		return nil
	}

	toolServer.Status.ObservedGeneration = toolServer.Generation
	toolServer.Status.DiscoveredTools = discoveredTools

	if err := a.kube.Status().Update(ctx, toolServer); err != nil {
		return fmt.Errorf("failed to update agent status: %v", err)
	}

	return nil
}

func (a *kagentReconciler) ReconcileKagentMemory(ctx context.Context, req ctrl.Request) error {
	memory := &v1alpha1.Memory{}
	if err := a.kube.Get(ctx, req.NamespacedName, memory); err != nil {
		if k8s_errors.IsNotFound(err) {
			return a.handleMemoryDeletion(req)
		}

		return fmt.Errorf("failed to get memory %s: %v", req.Name, err)
	}

	agents, err := a.findAgentsUsingMemory(ctx, req)
	if err != nil {
		return fmt.Errorf("failed to find agents using memory %s: %v", req.Name, err)
	}

	return a.reconcileMemoryStatus(ctx, memory, a.reconcileAgents(ctx, agents...))
}

func (a *kagentReconciler) handleMemoryDeletion(req ctrl.Request) error {

	// TODO(sbx0r): implement memory deletion

	return nil
}

func (a *kagentReconciler) reconcileMemoryStatus(ctx context.Context, memory *v1alpha1.Memory, err error) error {
	var (
		status  metav1.ConditionStatus
		message string
		reason  string
	)
	if err != nil {
		status = metav1.ConditionFalse
		message = err.Error()
		reason = "MemoryReconcileFailed"
		reconcileLog.Error(err, "failed to reconcile memory", "memory", utils.GetObjectRef(memory))
	} else {
		status = metav1.ConditionTrue
		reason = "MemoryReconciled"
	}

	conditionChanged := meta.SetStatusCondition(&memory.Status.Conditions, metav1.Condition{
		Type:               v1alpha1.MemoryConditionTypeAccepted,
		Status:             status,
		LastTransitionTime: metav1.Now(),
		Reason:             reason,
		Message:            message,
	})

	if conditionChanged || memory.Status.ObservedGeneration != memory.Generation {
		memory.Status.ObservedGeneration = memory.Generation
		if err := a.kube.Status().Update(ctx, memory); err != nil {
			return fmt.Errorf("failed to update memory status: %v", err)
		}
	}
	return nil
}

func (a *kagentReconciler) reconcileAgents(ctx context.Context, agents ...*v1alpha1.Agent) error {
	var multiErr *multierror.Error
	for _, agent := range agents {
		configHash, reconcileErr := a.reconcileAgent(ctx, agent)
		// Append error but still try to reconcile the agent status
		if reconcileErr != nil {
			multiErr = multierror.Append(multiErr, fmt.Errorf(
				"failed to reconcile agent %s/%s: %v", agent.Namespace, agent.Name, reconcileErr))
		}
		if err := a.reconcileAgentStatus(ctx, agent, configHash, reconcileErr); err != nil {
			multiErr = multierror.Append(multiErr, fmt.Errorf(
				"failed to reconcile agent status %s/%s: %v", agent.Namespace, agent.Name, err))
		}
	}

	return multiErr.ErrorOrNil()
}

func (a *kagentReconciler) reconcileAgent(ctx context.Context, agent *v1alpha1.Agent) (*[sha256.Size]byte, error) {
	agentOutputs, err := a.adkTranslator.TranslateAgent(ctx, agent)
	if err != nil {
		return nil, fmt.Errorf("failed to translate agent %s/%s: %v", agent.Namespace, agent.Name, err)
	}
	if err := a.reconcileA2A(ctx, agent, agentOutputs.Config); err != nil {
		return nil, fmt.Errorf("failed to reconcile A2A for agent %s/%s: %v", agent.Namespace, agent.Name, err)
	}
	if err := a.upsertAgent(ctx, agent, agentOutputs); err != nil {
		return nil, fmt.Errorf("failed to upsert agent %s/%s: %v", agent.Namespace, agent.Name, err)
	}

	return &agentOutputs.ConfigHash, nil
}

func (a *kagentReconciler) reconcileToolServer(ctx context.Context, server *v1alpha1.ToolServer) error {
	toolServer, err := a.adkTranslator.TranslateToolServer(ctx, server)
	if err != nil {
		return fmt.Errorf("failed to translate tool server %s/%s: %v", server.Namespace, server.Name, err)
	}
	err = a.upsertToolServer(ctx, toolServer)
	if err != nil {
		return fmt.Errorf("failed to upsert tool server %s/%s: %v", server.Namespace, server.Name, err)
	}

	return nil
}

func (a *kagentReconciler) upsertAgent(ctx context.Context, agent *v1alpha1.Agent, agentOutputs *translator.AgentOutputs) error {
	// lock to prevent races
	a.upsertLock.Lock()
	defer a.upsertLock.Unlock()

	dbAgent := &database.Agent{
		ID:     agentOutputs.Config.Name,
		Config: agentOutputs.Config,
	}

	if err := a.dbClient.StoreAgent(dbAgent); err != nil {
		return fmt.Errorf("failed to store agent %s: %v", agentOutputs.Config.Name, err)
	}

	// If the config hash has not changed, we can skip the patch
	if bytes.Equal(agentOutputs.ConfigHash[:], agent.Status.ConfigHash) {
		return nil
	}

	for _, obj := range agentOutputs.Manifest {
		if err := a.kube.Patch(ctx, obj, client.Apply, &client.PatchOptions{
			FieldManager: "kagent-controller",
			Force:        ptr.To(true),
		}); err != nil {
			return fmt.Errorf("failed to patch agent output %s: %v", agentOutputs.Config.Name, err)
		}
	}

	return nil
}

func (a *kagentReconciler) upsertToolServer(ctx context.Context, toolServer *database.ToolServer) error {
	// lock to prevent races
	a.upsertLock.Lock()
	defer a.upsertLock.Unlock()

	if _, err := a.dbClient.StoreToolServer(toolServer); err != nil {
		return fmt.Errorf("failed to store toolServer %s: %v", toolServer.Name, err)
	}

	toolServer, err := a.dbClient.GetToolServer(toolServer.Name)
	if err != nil {
		return fmt.Errorf("failed to get toolServer %s: %v", toolServer.Name, err)
	}

	var tools []*v1alpha1.MCPTool
	switch {
	case toolServer.Config.Sse != nil:
		sseHttpClient, err := transport.NewSSE(toolServer.Config.Sse.URL)
		if err != nil {
			return fmt.Errorf("failed to create sse client for toolServer %s: %v", toolServer.Name, err)
		}
		tools, err = a.listTools(ctx, sseHttpClient, toolServer)
		if err != nil {
			return fmt.Errorf("failed to fetch tools for toolServer %s: %v", toolServer.Name, err)
		}
	case toolServer.Config.StreamableHttp != nil:
		streamableHttpClient, err := transport.NewStreamableHTTP(toolServer.Config.StreamableHttp.URL)
		if err != nil {
			return fmt.Errorf("failed to create streamable http client for toolServer %s: %v", toolServer.Name, err)
		}
		tools, err = a.listTools(ctx, streamableHttpClient, toolServer)
		if err != nil {
			return fmt.Errorf("failed to fetch tools for toolServer %s: %v", toolServer.Name, err)
		}
	case toolServer.Config.Stdio != nil:
		// Can't list tools for stdio
		return fmt.Errorf("stdio tool servers are not supported")
	default:
		return fmt.Errorf("unsupported tool server type: %v", toolServer.Config.Type)
	}

	if err := a.dbClient.RefreshToolsForServer(toolServer.Name, tools...); err != nil {
		return fmt.Errorf("failed to refresh tools for toolServer %s: %v", toolServer.Name, err)
	}

	return nil
}

func (a *kagentReconciler) listTools(ctx context.Context, tsp transport.Interface, toolServer *database.ToolServer) ([]*v1alpha1.MCPTool, error) {
	client := mcp_client.NewClient(tsp)
	err := client.Start(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to start client for toolServer %s: %v", toolServer.Name, err)
	}
	defer client.Close()
	_, err = client.Initialize(ctx, mcp.InitializeRequest{
		Params: mcp.InitializeParams{
			ProtocolVersion: mcp.LATEST_PROTOCOL_VERSION,
			Capabilities:    mcp.ClientCapabilities{},
			ClientInfo: mcp.Implementation{
				Name:    "kagent-controller",
				Version: version.Version,
			},
		},
	})
	if err != nil {
		return nil, fmt.Errorf("failed to initialize client for toolServer %s: %v", toolServer.Name, err)
	}
	result, err := client.ListTools(ctx, mcp.ListToolsRequest{})
	if err != nil {
		return nil, fmt.Errorf("failed to list tools for toolServer %s: %v", toolServer.Name, err)
	}

	tools := make([]*v1alpha1.MCPTool, 0, len(result.Tools))
	for _, tool := range result.Tools {
		tools = append(tools, &v1alpha1.MCPTool{
			Name:        tool.Name,
			Description: tool.Description,
		})
	}

	return tools, nil
}

func (a *kagentReconciler) findAgentsUsingModel(ctx context.Context, req ctrl.Request) ([]*v1alpha1.Agent, error) {
	var agentsList v1alpha1.AgentList
	if err := a.kube.List(
		ctx,
		&agentsList,
	); err != nil {
		return nil, fmt.Errorf("failed to list agents: %v", err)
	}

	var agents []*v1alpha1.Agent
	for i := range agentsList.Items {
		agent := &agentsList.Items[i]
		agentNamespaced, err := utils.ParseRefString(agent.Spec.ModelConfig, agent.Namespace)

		if err != nil {
			reconcileLog.Error(err, "failed to parse Agent ModelConfig",
				"errorDetails", err.Error(),
			)
			continue
		}

		if agentNamespaced == req.NamespacedName {
			agents = append(agents, agent)
		}
	}

	return agents, nil
}

func (a *kagentReconciler) findAgentsUsingApiKeySecret(ctx context.Context, req ctrl.Request) ([]*v1alpha1.Agent, error) {
	var modelsList v1alpha1.ModelConfigList
	if err := a.kube.List(
		ctx,
		&modelsList,
	); err != nil {
		return nil, fmt.Errorf("failed to list ModelConfigs: %v", err)
	}

	var models []string
	for _, model := range modelsList.Items {
		if model.Spec.APIKeySecretRef == "" {
			continue
		}
		secretNamespaced, err := utils.ParseRefString(model.Spec.APIKeySecretRef, model.Namespace)
		if err != nil {
			reconcileLog.Error(err, "failed to parse ModelConfig APIKeySecretRef",
				"errorDetails", err.Error(),
			)
			continue
		}

		if secretNamespaced == req.NamespacedName {
			models = append(models, model.Name)
		}
	}

	var agents []*v1alpha1.Agent
	uniqueAgents := make(map[string]bool)

	for _, modelName := range models {
		agentsUsingModel, err := a.findAgentsUsingModel(ctx, ctrl.Request{
			NamespacedName: types.NamespacedName{
				Namespace: req.Namespace,
				Name:      modelName,
			},
		})
		if err != nil {
			return nil, fmt.Errorf("failed to find agents for model %s: %v", modelName, err)
		}

		for _, agent := range agentsUsingModel {
			key := utils.GetObjectRef(agent)
			if !uniqueAgents[key] {
				uniqueAgents[key] = true
				agents = append(agents, agent)
			}
		}
	}

	return agents, nil
}

func (a *kagentReconciler) findAgentsUsingMemory(ctx context.Context, req ctrl.Request) ([]*v1alpha1.Agent, error) {
	var agentsList v1alpha1.AgentList
	if err := a.kube.List(
		ctx,
		&agentsList,
	); err != nil {
		return nil, fmt.Errorf("failed to list agents: %v", err)
	}

	var agents []*v1alpha1.Agent
	for i := range agentsList.Items {
		agent := &agentsList.Items[i]
		for _, memory := range agent.Spec.Memory {
			memoryNamespaced, err := utils.ParseRefString(memory, agent.Namespace)

			if err != nil {
				reconcileLog.Error(err, "failed to parse Agent Memory",
					"errorDetails", err.Error(),
				)
				continue
			}

			if memoryNamespaced == req.NamespacedName {
				agents = append(agents, agent)
				break
			}
		}
	}

	return agents, nil
}

func (a *kagentReconciler) findAgentsUsingToolServer(ctx context.Context, req ctrl.Request) ([]*v1alpha1.Agent, error) {
	var agentsList v1alpha1.AgentList
	if err := a.kube.List(
		ctx,
		&agentsList,
	); err != nil {
		return nil, fmt.Errorf("failed to list agents: %v", err)
	}

	var agents []*v1alpha1.Agent
	appendAgentIfUsesToolServer := func(agent *v1alpha1.Agent) {
		for _, tool := range agent.Spec.Tools {
			if tool.McpServer == nil {
				return
			}

			toolServerNamespaced, err := utils.ParseRefString(tool.McpServer.ToolServer, agent.Namespace)
			if err != nil {
				reconcileLog.Error(err, "failed to parse Agent ToolServer",
					"errorDetails", err.Error(),
				)
				continue
			}

			if toolServerNamespaced == req.NamespacedName {
				agents = append(agents, agent)
				return
			}
		}
	}

	for _, agent := range agentsList.Items {
		agent := agent
		appendAgentIfUsesToolServer(&agent)
	}

	return agents, nil

}

func (a *kagentReconciler) getDiscoveredMCPTools(ctx context.Context, serverRef string) ([]*v1alpha1.MCPTool, error) {
	allTools, err := a.dbClient.ListToolsForServer(serverRef)
	if err != nil {
		return nil, err
	}

	var discoveredTools []*v1alpha1.MCPTool
	for _, tool := range allTools {
		mcpTool, err := convertTool(&tool)
		if err != nil {
			return nil, fmt.Errorf("failed to convert tool: %v", err)
		}
		discoveredTools = append(discoveredTools, mcpTool)
	}

	return discoveredTools, nil
}

func (a *kagentReconciler) reconcileA2A(
	ctx context.Context,
	agent *v1alpha1.Agent,
	adkConfig *adk.AgentConfig,
) error {
	return a.a2aReconciler.ReconcileAgent(ctx, agent, adkConfig)
}

func convertTool(tool *database.Tool) (*v1alpha1.MCPTool, error) {
	return &v1alpha1.MCPTool{
		Name:        tool.ID,
		Description: tool.Description,
	}, nil
}
