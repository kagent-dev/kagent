package reconciler

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"reflect"
	"slices"
	"strings"
	"sync"

	"github.com/hashicorp/go-multierror"
	reconcilerutils "github.com/kagent-dev/kagent/go/internal/controller/reconciler/utils"
	"github.com/kagent-dev/kmcp/api/v1alpha1"
	appsv1 "k8s.io/api/apps/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/util/retry"

	"github.com/kagent-dev/kagent/go/api/v1alpha2"
	"github.com/kagent-dev/kagent/go/internal/controller/translator"
	agent_translator "github.com/kagent-dev/kagent/go/internal/controller/translator/agent"
	"github.com/kagent-dev/kagent/go/internal/database"
	"github.com/kagent-dev/kagent/go/internal/utils"
	"github.com/kagent-dev/kagent/go/internal/version"
	mcp_client "github.com/mark3labs/mcp-go/client"
	"github.com/mark3labs/mcp-go/client/transport"
	"github.com/mark3labs/mcp-go/mcp"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
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
	GetOwnedResourceTypes() []client.Object
}

type kagentReconciler struct {
	adkTranslator agent_translator.AdkApiTranslator

	kube     client.Client
	dbClient database.Client

	defaultModelConfig types.NamespacedName

	// TODO: Remove this lock since we have a DB which we can batch anyway
	upsertLock sync.Mutex
}

func NewKagentReconciler(
	translator agent_translator.AdkApiTranslator,
	kube client.Client,
	dbClient database.Client,
	defaultModelConfig types.NamespacedName,
) KagentReconciler {
	return &kagentReconciler{
		adkTranslator:      translator,
		kube:               kube,
		dbClient:           dbClient,
		defaultModelConfig: defaultModelConfig,
	}
}

func (a *kagentReconciler) ReconcileKagentAgent(ctx context.Context, req ctrl.Request) error {
	// TODO(sbx0r): missing finalizer logic
	agent := &v1alpha2.Agent{}
	if err := a.kube.Get(ctx, req.NamespacedName, agent); err != nil {
		if apierrors.IsNotFound(err) {
			return a.handleAgentDeletion(req)
		}

		return fmt.Errorf("failed to get agent %s: %w", req.NamespacedName, err)
	}

	err := a.reconcileAgent(ctx, agent)
	if err != nil {
		reconcileLog.Error(err, "failed to reconcile agent", "agent", req.NamespacedName)
	}

	return a.reconcileAgentStatus(ctx, agent, err)
}

func (a *kagentReconciler) handleAgentDeletion(req ctrl.Request) error {
	if err := a.dbClient.DeleteAgent(req.String()); err != nil {
		return fmt.Errorf("failed to delete agent %s: %w",
			req.String(), err)
	}

	reconcileLog.Info("Agent was deleted", "namespace", req.Namespace, "name", req.Name)
	return nil
}

func (a *kagentReconciler) reconcileAgentStatus(ctx context.Context, agent *v1alpha2.Agent, err error) error {
	var (
		status  metav1.ConditionStatus
		message string
		reason  string
	)
	if err != nil {
		status = metav1.ConditionFalse
		message = err.Error()
		reason = "ReconcileFailed"
	} else {
		status = metav1.ConditionTrue
		reason = "Reconciled"
	}

	conditionChanged := meta.SetStatusCondition(&agent.Status.Conditions, metav1.Condition{
		Type:               v1alpha2.AgentConditionTypeAccepted,
		Status:             status,
		Reason:             reason,
		Message:            message,
		ObservedGeneration: agent.Generation,
	})

	deployedCondition := metav1.Condition{
		Type:               v1alpha2.AgentConditionTypeReady,
		Status:             metav1.ConditionUnknown,
		ObservedGeneration: agent.Generation,
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
		if deployment.Status.AvailableReplicas >= replicas {
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

	// update the status if it has changed or the generation has changed
	if conditionChanged || agent.Status.ObservedGeneration != agent.Generation {
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
		if apierrors.IsNotFound(err) {
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

	if remoteService, err := agent_translator.ConvertServiceToRemoteMCPServer(service); err != nil {
		reconcileLog.Error(err, "failed to convert service to remote mcp service", "service", utils.GetObjectRef(service))
	} else {
		if _, err := a.upsertToolServerForRemoteMCPServer(ctx, dbService, remoteService, service.Namespace); err != nil {
			return fmt.Errorf("failed to upsert tool server for mcp service %s: %v", utils.GetObjectRef(service), err)
		}
	}

	return nil
}

type secretRef struct {
	NamespacedName types.NamespacedName
	Secret         *corev1.Secret
}

func (a *kagentReconciler) ReconcileKagentModelConfig(ctx context.Context, req ctrl.Request) error {
	modelConfig := &v1alpha2.ModelConfig{}
	if err := a.kube.Get(ctx, req.NamespacedName, modelConfig); err != nil {
		if apierrors.IsNotFound(err) {
			return nil
		}

		return fmt.Errorf("failed to get model %s: %v", req.Name, err)
	}

	var err error
	var secrets []secretRef

	// check for api key secret
	if modelConfig.Spec.APIKeySecret != "" {
		secret := &corev1.Secret{}
		namespacedName := types.NamespacedName{Namespace: modelConfig.Namespace, Name: modelConfig.Spec.APIKeySecret}

		if kubeErr := a.kube.Get(ctx, namespacedName, secret); kubeErr != nil {
			err = multierror.Append(err, fmt.Errorf("failed to get secret %s: %v", modelConfig.Spec.APIKeySecret, kubeErr))
		} else {
			secrets = append(secrets, secretRef{
				NamespacedName: namespacedName,
				Secret:         secret,
			})
		}
	}

	// check for tls cert secret
	if modelConfig.Spec.TLS != nil && modelConfig.Spec.TLS.CACertSecretRef != "" {
		secret := &corev1.Secret{}
		namespacedName := types.NamespacedName{Namespace: modelConfig.Namespace, Name: modelConfig.Spec.TLS.CACertSecretRef}

		if kubeErr := a.kube.Get(ctx, namespacedName, secret); kubeErr != nil {
			err = multierror.Append(err, fmt.Errorf("failed to get secret %s: %v", modelConfig.Spec.TLS.CACertSecretRef, kubeErr))
		} else {
			secrets = append(secrets, secretRef{
				NamespacedName: namespacedName,
				Secret:         secret,
			})
		}
	}

	// compute the hash for the status
	secretHash := computeStatusSecretHash(secrets)

	return a.reconcileModelConfigStatus(
		ctx,
		modelConfig,
		err,
		secretHash,
	)
}

// computeStatusSecretHash computes a deterministic singular hash of the secrets the model config references for the status
// this loses per-secret context (i.e. versioning/hash status per-secret), but simplifies the number of statuses tracked
func computeStatusSecretHash(secrets []secretRef) string {
	// sort secret references for deterministic output
	slices.SortStableFunc(secrets, func(a, b secretRef) int {
		return strings.Compare(a.NamespacedName.String(), b.NamespacedName.String())
	})

	// compute a singular hash of the secrets
	// this loses per-secret context (i.e. versioning/hash status per-secret), but simplifies the number of statuses tracked
	hash := sha256.New()
	for _, s := range secrets {
		hash.Write([]byte(s.NamespacedName.String()))

		keys := make([]string, 0, len(s.Secret.Data))
		for k := range s.Secret.Data {
			keys = append(keys, k)
		}
		slices.Sort(keys)

		for _, k := range keys {
			hash.Write([]byte(k))
			hash.Write(s.Secret.Data[k])
		}
	}

	return hex.EncodeToString(hash.Sum(nil))
}

func (a *kagentReconciler) reconcileModelConfigStatus(ctx context.Context, modelConfig *v1alpha2.ModelConfig, err error, secretHash string) error {
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

	// check if the secret hash has changed
	secretHashChanged := modelConfig.Status.SecretHash != secretHash
	if secretHashChanged {
		modelConfig.Status.SecretHash = secretHash
	}

	// update the status if it has changed or the generation has changed
	if conditionChanged || modelConfig.Status.ObservedGeneration != modelConfig.Generation || secretHashChanged {
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
		if apierrors.IsNotFound(err) {
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

	if remoteSpec, err := agent_translator.ConvertMCPServerToRemoteMCPServer(mcpServer); err != nil {
		reconcileLog.Error(err, "failed to convert mcp server to remote mcp server", "mcpServer", utils.GetObjectRef(mcpServer))
	} else {
		if _, err := a.upsertToolServerForRemoteMCPServer(ctx, dbServer, remoteSpec, mcpServer.Namespace); err != nil {
			return fmt.Errorf("failed to upsert tool server for remote mcp server %s: %v", utils.GetObjectRef(mcpServer), err)
		}
	}

	return nil
}

func (a *kagentReconciler) ReconcileKagentRemoteMCPServer(ctx context.Context, req ctrl.Request) error {
	nns := req.NamespacedName
	serverRef := nns.String()
	l := reconcileLog.WithValues("remoteMCPServer", serverRef)

	server := &v1alpha2.RemoteMCPServer{}
	if err := a.kube.Get(ctx, nns, server); err != nil {
		// if the remote MCP server is not found, we can ignore it
		if apierrors.IsNotFound(err) {
			// Delete from DB if the remote mcp server is deleted
			dbServer := &database.ToolServer{
				Name:      serverRef,
				GroupKind: schema.GroupKind{Group: "kagent.dev", Kind: "RemoteMCPServer"}.String(),
			}

			if err := a.dbClient.DeleteToolServer(dbServer.Name, dbServer.GroupKind); err != nil {
				l.Error(err, "failed to delete tool server for remote mcp server")
			}

			if err := a.dbClient.DeleteToolsForServer(dbServer.Name, dbServer.GroupKind); err != nil {
				l.Error(err, "failed to delete tools for remote mcp server")
			}

			return nil
		}

		return fmt.Errorf("failed to get remote mcp server %s: %v", serverRef, err)
	}

	dbServer := &database.ToolServer{
		Name:        serverRef,
		Description: server.Spec.Description,
		GroupKind:   server.GroupVersionKind().GroupKind().String(),
	}

	tools, err := a.upsertToolServerForRemoteMCPServer(ctx, dbServer, &server.Spec, server.Namespace)
	if err != nil {
		l.Error(err, "failed to upsert tool server for remote mcp server")

		// Fetch previously discovered tools from database if possible
		var discoveryErr error
		tools, discoveryErr = a.getDiscoveredMCPTools(ctx, serverRef)
		if discoveryErr != nil {
			err = multierror.Append(err, discoveryErr)
		}
	}

	// update the tool server status as the agents depend on it
	if err := a.reconcileRemoteMCPServerStatus(
		ctx,
		server,
		tools,
		err,
	); err != nil {
		return fmt.Errorf("failed to reconcile remote mcp server status %s: %v", req.NamespacedName, err)
	}

	return nil
}

func (a *kagentReconciler) reconcileRemoteMCPServerStatus(
	ctx context.Context,
	server *v1alpha2.RemoteMCPServer,
	discoveredTools []*v1alpha2.MCPTool,
	err error,
) error {
	var (
		status  metav1.ConditionStatus
		message string
		reason  string
	)
	if err != nil {
		status = metav1.ConditionFalse
		message = err.Error()
		reason = "ReconcileFailed"
	} else {
		status = metav1.ConditionTrue
		reason = "Reconciled"
	}
	conditionChanged := meta.SetStatusCondition(&server.Status.Conditions, metav1.Condition{
		Type:               v1alpha2.AgentConditionTypeAccepted,
		Status:             status,
		Reason:             reason,
		Message:            message,
		ObservedGeneration: server.Generation,
	})

	// only update if the status has changed to prevent looping the reconciler
	if !conditionChanged &&
		server.Status.ObservedGeneration == server.Generation &&
		reflect.DeepEqual(server.Status.DiscoveredTools, discoveredTools) {
		return nil
	}

	server.Status.ObservedGeneration = server.Generation
	server.Status.DiscoveredTools = discoveredTools

	if err := a.kube.Status().Update(ctx, server); err != nil {
		return fmt.Errorf("failed to update remote mcp server status: %v", err)
	}

	return nil
}

func (a *kagentReconciler) reconcileAgent(ctx context.Context, agent *v1alpha2.Agent) error {
	agentOutputs, err := a.adkTranslator.TranslateAgent(ctx, agent)
	if err != nil {
		return fmt.Errorf("failed to translate agent %s/%s: %v", agent.Namespace, agent.Name, err)
	}

	ownedObjects, err := reconcilerutils.FindOwnedObjects(ctx, a.kube, agent.UID, agent.Namespace, a.adkTranslator.GetOwnedResourceTypes())
	if err != nil {
		return err
	}

	if err := a.reconcileDesiredObjects(ctx, agent, agentOutputs.Manifest, ownedObjects); err != nil {
		return fmt.Errorf("failed to reconcile owned objects: %v", err)
	}

	if err := a.upsertAgent(ctx, agent, agentOutputs); err != nil {
		return fmt.Errorf("failed to upsert agent %s/%s: %v", agent.Namespace, agent.Name, err)
	}

	return nil
}

// GetOwnedResourceTypes returns all the resource types that may be owned by
// controllers that are reconciled herein. At present only the agents controller
// owns resources so this simply wraps a call to the ADK translator as that is
// responsible for creating the manifests for an agent. If in future other
// controllers start owning resources then this method should be updated to
// return the distinct union of all owned resource types.
func (r *kagentReconciler) GetOwnedResourceTypes() []client.Object {
	return r.adkTranslator.GetOwnedResourceTypes()
}

// Function initially copied from https://github.com/open-telemetry/opentelemetry-operator/blob/e6d96f006f05cff0bc3808da1af69b6b636fbe88/internal/controllers/common.go#L141-L192
func (a *kagentReconciler) reconcileDesiredObjects(ctx context.Context, owner metav1.Object, desiredObjects []client.Object, ownedObjects map[types.UID]client.Object) error {
	var errs []error
	for _, desired := range desiredObjects {
		l := reconcileLog.WithValues(
			"object_name", desired.GetName(),
			"object_kind", desired.GetObjectKind(),
		)

		// existing is an object the controller runtime will hydrate for us
		// we obtain the existing object by deep copying the desired object because it's the most convenient way
		existing := desired.DeepCopyObject().(client.Object)
		mutateFn := translator.MutateFuncFor(existing, desired)

		if err := retry.RetryOnConflict(retry.DefaultRetry, func() error {
			_, createOrUpdateErr := createOrUpdate(ctx, a.kube, existing, mutateFn)
			return createOrUpdateErr
		}); err != nil {
			l.Error(err, "failed to configure desired")
			errs = append(errs, err)
			continue
		}

		// This object is still managed by the controller, remove it from the list of objects to prune
		delete(ownedObjects, existing.GetUID())
	}

	if len(errs) > 0 {
		return fmt.Errorf("failed to create objects for %s: %w", owner.GetName(), errors.Join(errs...))
	}

	// Pruning owned objects in the cluster which are not should not be present after the reconciliation.
	err := a.deleteObjects(ctx, ownedObjects)
	if err != nil {
		return fmt.Errorf("failed to prune objects for %s: %w", owner.GetName(), err)
	}

	return nil
}

// modified version of controllerutil.CreateOrUpdate to support proto based objects like istio
func createOrUpdate(ctx context.Context, c client.Client, obj client.Object, f controllerutil.MutateFn) (controllerutil.OperationResult, error) {
	key := client.ObjectKeyFromObject(obj)
	if err := c.Get(ctx, key, obj); err != nil {
		if !apierrors.IsNotFound(err) {
			return controllerutil.OperationResultNone, err
		}
		if f != nil {
			if err := mutate(f, key, obj); err != nil {
				return controllerutil.OperationResultNone, err
			}
		}

		if err := c.Create(ctx, obj); err != nil {
			return controllerutil.OperationResultNone, err
		}
		return controllerutil.OperationResultCreated, nil
	}

	existing := obj.DeepCopyObject()
	if f != nil {
		if err := mutate(f, key, obj); err != nil {
			return controllerutil.OperationResultNone, err
		}
	}

	// special equality function to handle proto based crds
	if reconcilerutils.ObjectsEqual(existing, obj) {
		return controllerutil.OperationResultNone, nil
	}

	if err := c.Update(ctx, obj); err != nil {
		return controllerutil.OperationResultNone, err
	}

	return controllerutil.OperationResultUpdated, nil
}

// mutate wraps a MutateFn and applies validation to its result.
func mutate(f controllerutil.MutateFn, key client.ObjectKey, obj client.Object) error {
	if err := f(); err != nil {
		return err
	}
	if newKey := client.ObjectKeyFromObject(obj); key != newKey {
		return fmt.Errorf("MutateFn cannot mutate object name and/or object namespace")
	}
	return nil
}

func (a *kagentReconciler) deleteObjects(ctx context.Context, objects map[types.UID]client.Object) error {
	// Pruning owned objects in the cluster which are not should not be present after the reconciliation.
	pruneErrs := []error{}

	for _, obj := range objects {
		l := reconcileLog.WithValues(
			"object_name", obj.GetName(),
			"object_kind", obj.GetObjectKind().GroupVersionKind(),
		)

		l.Info("pruning unmanaged resource")
		err := a.kube.Delete(ctx, obj)
		if err != nil {
			l.Error(err, "failed to delete resource")
			pruneErrs = append(pruneErrs, err)
		}
	}

	return errors.Join(pruneErrs...)
}

func (a *kagentReconciler) upsertAgent(ctx context.Context, agent *v1alpha2.Agent, agentOutputs *agent_translator.AgentOutputs) error {
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

	return nil
}

func (a *kagentReconciler) upsertToolServerForRemoteMCPServer(ctx context.Context, toolServer *database.ToolServer, remoteMcpServer *v1alpha2.RemoteMCPServerSpec, namespace string) ([]*v1alpha2.MCPTool, error) {
	// lock to prevent races
	a.upsertLock.Lock()
	defer a.upsertLock.Unlock()

	if _, err := a.dbClient.StoreToolServer(toolServer); err != nil {
		return nil, fmt.Errorf("failed to store toolServer %s: %v", toolServer.Name, err)
	}

	tsp, err := a.createMcpTransport(ctx, remoteMcpServer, namespace)
	if err != nil {
		return nil, fmt.Errorf("failed to create client for toolServer %s: %v", toolServer.Name, err)
	}

	tools, err := a.listTools(ctx, tsp, toolServer)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch tools for toolServer %s: %v", toolServer.Name, err)
	}

	if err := a.dbClient.RefreshToolsForServer(toolServer.Name, toolServer.GroupKind, tools...); err != nil {
		return nil, fmt.Errorf("failed to refresh tools for toolServer %s: %v", toolServer.Name, err)
	}

	return tools, nil
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

func convertTool(tool *database.Tool) (*v1alpha2.MCPTool, error) {
	return &v1alpha2.MCPTool{
		Name:        tool.ID,
		Description: tool.Description,
	}, nil
}
