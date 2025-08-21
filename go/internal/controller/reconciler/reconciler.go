package reconciler

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"reflect"
	"sync"

	"github.com/hashicorp/go-multierror"
	appsv1 "k8s.io/api/apps/v1"
	k8s_errors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/utils/ptr"
	"trpc.group/trpc-go/trpc-a2a-go/server"

	"github.com/kagent-dev/kagent/go/api/v1alpha2"
	"github.com/kagent-dev/kagent/go/internal/controller/a2a"
	"github.com/kagent-dev/kagent/go/internal/controller/translator"
	"github.com/kagent-dev/kagent/go/internal/database"
	"github.com/kagent-dev/kagent/go/internal/utils"
	"github.com/kagent-dev/kagent/go/internal/version"
	"github.com/kagent-dev/kmcp/api/v1alpha1"
	mcp_client "github.com/mark3labs/mcp-go/client"
	"github.com/mark3labs/mcp-go/client/transport"
	"github.com/mark3labs/mcp-go/mcp"
	corev1 "k8s.io/api/core/v1"
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
	ReconcileKagentRemoteMCPServer(ctx context.Context, req ctrl.Request) error
	ReconcileKagentMCPService(ctx context.Context, req ctrl.Request) error
	ReconcileKagentMCPServer(ctx context.Context, req ctrl.Request) error
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

	agent := &v1alpha2.Agent{}
	if err := a.kube.Get(ctx, req.NamespacedName, agent); err != nil {
		if k8s_errors.IsNotFound(err) {
			return a.handleAgentDeletion(req)
		}

		return fmt.Errorf("failed to get agent %s/%s: %w", req.Namespace, req.Name, err)
	}

	return a.handleExistingAgent(ctx, agent, req)
}

func (a *kagentReconciler) handleAgentDeletion(req ctrl.Request) error {
	// remove a2a handler if it exists
	a.a2aReconciler.ReconcileAgentDeletion(req.String())

	if err := a.dbClient.DeleteAgent(req.String()); err != nil {
		return fmt.Errorf("failed to delete agent %s: %w",
			req.String(), err)
	}

	reconcileLog.Info("Agent was deleted", "namespace", req.Namespace, "name", req.Name)
	return nil
}

func (a *kagentReconciler) handleExistingAgent(ctx context.Context, agent *v1alpha2.Agent, req ctrl.Request) error {
	reconcileLog.Info("Agent Event",
		"namespace", req.Namespace,
		"name", req.Name,
		"oldGeneration", agent.Status.ObservedGeneration,
		"newGeneration", agent.Generation)

	var multiErr *multierror.Error

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

	return multiErr.ErrorOrNil()
}

func (a *kagentReconciler) reconcileAgentStatus(ctx context.Context, agent *v1alpha2.Agent, configHash []byte, inputErr error) error {
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
		Type:               v1alpha2.AgentConditionTypeAccepted,
		Status:             status,
		LastTransitionTime: metav1.Now(),
		Reason:             reason,
		Message:            message,
	})

	deployedCondition := metav1.Condition{
		Type:               v1alpha2.AgentConditionTypeReady,
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

	conditionChanged = conditionChanged || meta.SetStatusCondition(&agent.Status.Conditions, deployedCondition)

	// Only update the config hash if the config hash has changed and there was no error
	configHashChanged := len(configHash) > 0 && !bytes.Equal((agent.Status.ConfigHash)[:], configHash[:])

	// update the status if it has changed or the generation has changed
	if conditionChanged || agent.Status.ObservedGeneration != agent.Generation || configHashChanged {
		// If the config hash is nil, it means there was an error during the reconciliation
		if configHashChanged {
			agent.Status.ConfigHash = configHash[:]
		}
		agent.Status.ObservedGeneration = agent.Generation
		if err := a.kube.Status().Update(ctx, agent); err != nil {
			return fmt.Errorf("failed to update agent status: %v", err)
		}
	}
	return nil
}

func (a *kagentReconciler) ReconcileKagentMCPService(ctx context.Context, req ctrl.Request) error {
	service := &corev1.Service{}
	if err := a.kube.Get(ctx, req.NamespacedName, service); err != nil {
		if k8s_errors.IsNotFound(err) {
			// Delete from DB if the service is deleted
			dbService := &database.ToolServer{
				Name:      req.String(),
				GroupKind: schema.GroupKind{Group: "", Kind: "Service"}.String(),
			}
			if err := a.dbClient.DeleteToolServer(dbService.Name, dbService.GroupKind); err != nil {
				reconcileLog.Error(err, "failed to delete tool server for mcp service", "service", req.String())
			}
			reconcileLog.Info("mcp service was deleted", "service", req.String())
			if err := a.dbClient.DeleteToolsForServer(dbService.Name, dbService.GroupKind); err != nil {
				reconcileLog.Error(err, "failed to delete tools for mcp service", "service", req.String())
			}
			return nil
		}
		return fmt.Errorf("failed to get service %s: %v", req.Name, err)
	}

	dbService := &database.ToolServer{
		Name:        utils.GetObjectRef(service),
		Description: "N/A",
		GroupKind:   schema.GroupKind{Group: "", Kind: "Service"}.String(),
	}

	if remoteService, err := translator.ConvertServiceToRemoteMCPServer(service); err != nil {
		reconcileLog.Error(err, "failed to convert service to remote mcp service", "service", utils.GetObjectRef(service))
	} else {
		if err := a.upsertToolServerForRemoteMCPServer(ctx, dbService, remoteService, service.Namespace); err != nil {
			return fmt.Errorf("failed to upsert tool server for mcp service %s: %v", utils.GetObjectRef(service), err)
		}
	}
	return nil
}

func (a *kagentReconciler) ReconcileKagentModelConfig(ctx context.Context, req ctrl.Request) error {
	modelConfig := &v1alpha2.ModelConfig{}
	if err := a.kube.Get(ctx, req.NamespacedName, modelConfig); err != nil {
		if k8s_errors.IsNotFound(err) {
			return nil
		}

		return fmt.Errorf("failed to get model %s: %v", req.Name, err)
	}

	var err error
	if modelConfig.Spec.APIKeySecret != "" {
		secret := &corev1.Secret{}
		if err = a.kube.Get(ctx, types.NamespacedName{Namespace: modelConfig.Namespace, Name: modelConfig.Spec.APIKeySecret}, secret); err != nil {
			err = fmt.Errorf("failed to get secret %s: %v", modelConfig.Spec.APIKeySecret, err)
		}
	}

	return a.reconcileModelConfigStatus(
		ctx,
		modelConfig,
		err,
	)
}

func (a *kagentReconciler) reconcileModelConfigStatus(ctx context.Context, modelConfig *v1alpha2.ModelConfig, err error) error {
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
		Type:               v1alpha2.ModelConfigConditionTypeAccepted,
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

func (a *kagentReconciler) ReconcileKagentMCPServer(ctx context.Context, req ctrl.Request) error {
	mcpServer := &v1alpha1.MCPServer{}
	if err := a.kube.Get(ctx, req.NamespacedName, mcpServer); err != nil {
		if k8s_errors.IsNotFound(err) {
			// Delete from DB if the mcp server is deleted
			dbServer := &database.ToolServer{
				Name:      req.String(),
				GroupKind: schema.GroupKind{Group: "kagent.dev", Kind: "MCPServer"}.String(),
			}
			if err := a.dbClient.DeleteToolServer(dbServer.Name, dbServer.GroupKind); err != nil {
				reconcileLog.Error(err, "failed to delete tool server for mcp server", "mcpServer", req.String())
			}
			reconcileLog.Info("mcp server was deleted", "mcpServer", req.String())
			if err := a.dbClient.DeleteToolsForServer(dbServer.Name, dbServer.GroupKind); err != nil {
				reconcileLog.Error(err, "failed to delete tools for mcp server", "mcpServer", req.String())
			}
			return nil
		}
		return fmt.Errorf("failed to get mcp server %s: %v", req.Name, err)
	}

	dbServer := &database.ToolServer{
		Name:        utils.GetObjectRef(mcpServer),
		Description: "N/A",
		GroupKind:   schema.GroupKind{Group: "kagent.dev", Kind: "MCPServer"}.String(),
	}
	if remoteSpec, err := translator.ConvertMCPServerToRemoteMCPServer(mcpServer); err != nil {
		reconcileLog.Error(err, "failed to convert mcp server to remote mcp server", "mcpServer", utils.GetObjectRef(mcpServer))
	} else {
		if err := a.upsertToolServerForRemoteMCPServer(ctx, dbServer, remoteSpec, mcpServer.Namespace); err != nil {
			reconcileLog.Error(err, "failed to upsert tool server for remote mcp server", "mcpServer", utils.GetObjectRef(mcpServer))
		}
	}

	return nil
}

func (a *kagentReconciler) ReconcileKagentRemoteMCPServer(ctx context.Context, req ctrl.Request) error {
	// reconcile the agent team itself
	toolServer := &v1alpha2.RemoteMCPServer{}
	if err := a.kube.Get(ctx, req.NamespacedName, toolServer); err != nil {
		// if the tool server is not found, we can ignore it
		if k8s_errors.IsNotFound(err) {
			// Delete from DB if the remote mcp server is deleted
			dbServer := &database.ToolServer{
				Name:      req.String(),
				GroupKind: schema.GroupKind{Group: "kagent.dev", Kind: "RemoteMCPServer"}.String(),
			}
			if err := a.dbClient.DeleteToolServer(dbServer.Name, dbServer.GroupKind); err != nil {
				reconcileLog.Error(err, "failed to delete tool server for remote mcp server", "remoteMCPServer", req.String())
			}
			reconcileLog.Info("remote mcp server was deleted", "remoteMCPServer", req.String())
			if err := a.dbClient.DeleteToolsForServer(dbServer.Name, dbServer.GroupKind); err != nil {
				reconcileLog.Error(err, "failed to delete tools for remote mcp server", "remoteMCPServer", req.String())
			}
			return nil
		}
		return fmt.Errorf("failed to get tool server %s: %v", req.Name, err)
	}

	dbServer := &database.ToolServer{
		Name:        utils.GetObjectRef(toolServer),
		Description: toolServer.Spec.Description,
		GroupKind:   schema.GroupKind{Group: "kagent.dev", Kind: "RemoteMCPServer"}.String(),
	}
	reconcileErr := a.upsertToolServerForRemoteMCPServer(ctx, dbServer, &toolServer.Spec, toolServer.Namespace)

	// update the tool server status as the agents depend on it
	if err := a.reconcileRemoteMCPServerStatus(
		ctx,
		toolServer,
		utils.GetObjectRef(toolServer),
		reconcileErr,
	); err != nil {
		return fmt.Errorf("failed to reconcile tool server %s: %v", req.Name, err)
	}

	return nil
}

func (a *kagentReconciler) reconcileRemoteMCPServerStatus(
	ctx context.Context,
	toolServer *v1alpha2.RemoteMCPServer,
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
		Type:               v1alpha2.AgentConditionTypeAccepted,
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

func (a *kagentReconciler) reconcileAgent(ctx context.Context, agent *v1alpha2.Agent) ([]byte, error) {
	agentOutputs, err := a.adkTranslator.TranslateAgent(ctx, agent)
	if err != nil {
		return nil, fmt.Errorf("failed to translate agent %s/%s: %v", agent.Namespace, agent.Name, err)
	}

	agentJson, err := json.Marshal(agentOutputs)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal agent outputs: %v", err)
	}

	hash := sha256.Sum256(agentJson)

	if err := a.reconcileA2A(ctx, agent, agentOutputs.AgentCard); err != nil {
		return nil, fmt.Errorf("failed to reconcile A2A for agent %s/%s: %v", agent.Namespace, agent.Name, err)
	}
	if err := a.upsertAgent(ctx, agent, agentOutputs, hash[:]); err != nil {
		return nil, fmt.Errorf("failed to upsert agent %s/%s: %v", agent.Namespace, agent.Name, err)
	}

	return hash[:], nil
}

func (a *kagentReconciler) upsertAgent(ctx context.Context, agent *v1alpha2.Agent, agentOutputs *translator.AgentOutputs, configHash []byte) error {
	// lock to prevent races
	a.upsertLock.Lock()
	defer a.upsertLock.Unlock()

	id := utils.ConvertToPythonIdentifier(utils.GetObjectRef(agent))
	dbAgent := &database.Agent{
		ID:     id,
		Type:   string(agent.Spec.Type),
		Config: agentOutputs.Config,
	}

	if err := a.dbClient.StoreAgent(dbAgent); err != nil {
		return fmt.Errorf("failed to store agent %s: %v", id, err)
	}

	// If the config hash has not changed, we can skip the patch
	if bytes.Equal(configHash, agent.Status.ConfigHash) {
		return nil
	}

	for _, obj := range agentOutputs.Manifest {
		if err := a.kube.Patch(ctx, obj, client.Apply, &client.PatchOptions{
			FieldManager: "kagent-controller",
			Force:        ptr.To(true),
		}); err != nil {
			return fmt.Errorf("failed to patch agent output %s: %v", id, err)
		}
	}

	return nil
}

func (a *kagentReconciler) upsertToolServerForRemoteMCPServer(ctx context.Context, toolServer *database.ToolServer, remoteMcpServer *v1alpha2.RemoteMCPServerSpec, namespace string) error {
	// lock to prevent races
	a.upsertLock.Lock()
	defer a.upsertLock.Unlock()

	if _, err := a.dbClient.StoreToolServer(toolServer); err != nil {
		return fmt.Errorf("failed to store toolServer %s: %v", toolServer.Name, err)
	}

	tsp, err := a.createMcpTransport(ctx, remoteMcpServer, namespace)
	if err != nil {
		return fmt.Errorf("failed to create client for toolServer %s: %v", toolServer.Name, err)
	}

	tools, err := a.listTools(ctx, tsp, toolServer)
	if err != nil {
		return fmt.Errorf("failed to fetch tools for toolServer %s: %v", toolServer.Name, err)
	}

	if err := a.dbClient.RefreshToolsForServer(toolServer.Name, toolServer.GroupKind, tools...); err != nil {
		return fmt.Errorf("failed to refresh tools for toolServer %s: %v", toolServer.Name, err)
	}

	return nil
}

func (a *kagentReconciler) createMcpTransport(ctx context.Context, s *v1alpha2.RemoteMCPServerSpec, namespace string) (transport.Interface, error) {
	headers, err := s.ResolveHeaders(ctx, a.kube, namespace)
	if err != nil {
		return nil, err
	}

	switch s.Protocol {
	case v1alpha2.RemoteMCPServerProtocolSse:
		return transport.NewSSE(s.URL, transport.WithHeaders(headers))
	default:
		return transport.NewStreamableHTTP(s.URL, transport.WithHTTPHeaders(headers))
	}
}

func (a *kagentReconciler) listTools(ctx context.Context, tsp transport.Interface, toolServer *database.ToolServer) ([]*v1alpha2.MCPTool, error) {
	client := mcp_client.NewClient(tsp)
	err := client.Start(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to start client for toolServer %s: %v", toolServer.Name, err)
	}
	defer client.Close() //nolint:errcheck
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

	tools := make([]*v1alpha2.MCPTool, 0, len(result.Tools))
	for _, tool := range result.Tools {
		tools = append(tools, &v1alpha2.MCPTool{
			Name:        tool.Name,
			Description: tool.Description,
		})
	}

	return tools, nil
}

func (a *kagentReconciler) getDiscoveredMCPTools(ctx context.Context, serverRef string) ([]*v1alpha2.MCPTool, error) {
	// This function is currently only used for RemoteMCPServer
	allTools, err := a.dbClient.ListToolsForServer(serverRef, schema.GroupKind{Group: "kagent.dev", Kind: "RemoteMCPServer"}.String())
	if err != nil {
		return nil, err
	}

	var discoveredTools []*v1alpha2.MCPTool
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
	agent *v1alpha2.Agent,
	card server.AgentCard,
) error {
	return a.a2aReconciler.ReconcileAgent(ctx, agent, card)
}

func convertTool(tool *database.Tool) (*v1alpha2.MCPTool, error) {
	return &v1alpha2.MCPTool{
		Name:        tool.ID,
		Description: tool.Description,
	}, nil
}
