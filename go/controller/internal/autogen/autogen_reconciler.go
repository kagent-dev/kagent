package autogen

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"sync"

	"github.com/hashicorp/go-multierror"
	"github.com/kagent-dev/kagent/go/autogen/api"
	"k8s.io/apimachinery/pkg/api/errors"
	k8s_errors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	autogen_client "github.com/kagent-dev/kagent/go/autogen/client"
	"github.com/kagent-dev/kagent/go/controller/api/v1alpha1"
	"github.com/kagent-dev/kagent/go/controller/internal/a2a"
	common "github.com/kagent-dev/kagent/go/controller/internal/utils"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

var (
	reconcileLog = ctrl.Log.WithName("reconciler")
)

type AutogenReconciler interface {
	ReconcileAutogenAgent(ctx context.Context, req ctrl.Request) error
	ReconcileAutogenModelConfig(ctx context.Context, req ctrl.Request) error
	ReconcileAutogenTeam(ctx context.Context, req ctrl.Request) error
	ReconcileAutogenApiKeySecret(ctx context.Context, req ctrl.Request) error
	ReconcileAutogenToolServer(ctx context.Context, req ctrl.Request) error
	ReconcileAutogenMemory(ctx context.Context, req ctrl.Request) error
}

type autogenReconciler struct {
	autogenTranslator ApiTranslator
	a2aReconciler     a2a.A2AReconciler

	kube          client.Client
	autogenClient autogen_client.Client

	defaultModelConfig types.NamespacedName
	upsertLock         sync.Mutex
}

func NewAutogenReconciler(
	translator ApiTranslator,
	kube client.Client,
	autogenClient autogen_client.Client,
	defaultModelConfig types.NamespacedName,
	a2aReconciler a2a.A2AReconciler,
) AutogenReconciler {
	return &autogenReconciler{
		autogenTranslator:  translator,
		kube:               kube,
		autogenClient:      autogenClient,
		defaultModelConfig: defaultModelConfig,
		a2aReconciler:      a2aReconciler,
	}
}

func (a *autogenReconciler) ReconcileAutogenAgent(ctx context.Context, req ctrl.Request) error {
	// reconcile the agent team itself

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

func (a *autogenReconciler) handleAgentDeletion(req ctrl.Request) error {
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
	a.a2aReconciler.ReconcileAutogenAgentDeletion(req.NamespacedName.String())

	// TODO(sbx0r): temporary mock on GlobalUserID.
	//              This block will be removed after resolving previous TODO
	team, err := a.autogenClient.GetTeam(req.NamespacedName.String(), common.GetGlobalUserID())
	if err != nil {
		return fmt.Errorf("failed to get agent on agent deletion %s/%s: %w",
			req.Namespace, req.Name, err)
	}

	if team != nil {
		if err = a.autogenClient.DeleteTeam(team.Id, team.UserID); err != nil {
			return fmt.Errorf("failed to delete agent %s: %w",
				req.NamespacedName.String(), err)
		}
	}

	reconcileLog.Info("Agent was deleted", "namespace", req.Namespace, "name", req.Name)
	return nil
}

func (a *autogenReconciler) handleExistingAgent(ctx context.Context, agent *v1alpha1.Agent, req ctrl.Request) error {
	isNewAgent := agent.Status.ObservedGeneration == 0
	isUpdatedAgent := agent.Generation > agent.Status.ObservedGeneration

	if isNewAgent {
		reconcileLog.Info("New agent was created",
			"namespace", req.Namespace,
			"name", req.Name,
			"generation", agent.Generation)
	} else if isUpdatedAgent {
		reconcileLog.Info("Agent was updated",
			"namespace", req.Namespace,
			"name", req.Name,
			"oldGeneration", agent.Status.ObservedGeneration,
			"newGeneration", agent.Generation)
	}

	if err := a.reconcileAgents(ctx, agent); err != nil {
		return fmt.Errorf("failed to reconcile agent %s/%s: %w",
			req.Namespace, req.Name, err)
	}

	teams, err := a.findTeamsUsingAgent(ctx, req)
	if err != nil {
		return fmt.Errorf("failed to find teams for agent %s/%s: %w",
			req.Namespace, req.Name, err)
	}

	return a.reconcileAgentStatus(ctx, agent, a.reconcileTeams(ctx, teams...))
}

func (a *autogenReconciler) reconcileAgentStatus(ctx context.Context, agent *v1alpha1.Agent, err error) error {
	var (
		status  metav1.ConditionStatus
		message string
		reason  string
	)
	if err != nil {
		status = metav1.ConditionFalse
		message = err.Error()
		reason = "AgentReconcileFailed"
		reconcileLog.Error(err, "failed to reconcile agent", "agent", agent)
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

	// update the status if it has changed or the generation has changed
	if conditionChanged || agent.Status.ObservedGeneration != agent.Generation {
		agent.Status.ObservedGeneration = agent.Generation
		if err := a.kube.Status().Update(ctx, agent); err != nil {
			return fmt.Errorf("failed to update agent status: %v", err)
		}
	}
	return nil
}

func (a *autogenReconciler) ReconcileAutogenModelConfig(ctx context.Context, req ctrl.Request) error {
	modelConfig := &v1alpha1.ModelConfig{}
	if err := a.kube.Get(ctx, req.NamespacedName, modelConfig); err != nil {
		return fmt.Errorf("failed to get model %s: %v", req.Name, err)
	}

	agents, err := a.findAgentsUsingModel(ctx, req)
	if err != nil {
		return fmt.Errorf("failed to find agents for model %s: %v", req.Name, err)
	}

	if err := a.reconcileAgents(ctx, agents...); err != nil {
		return fmt.Errorf("failed to reconcile agents for model %s: %v", req.Name, err)
	}

	teams, err := a.findTeamsUsingModel(ctx, req)
	if err != nil {
		return fmt.Errorf("failed to find teams for model %s: %v", req.Name, err)
	}

	return a.reconcileModelConfigStatus(
		ctx,
		modelConfig,
		a.reconcileTeams(ctx, teams...),
	)
}

func (a *autogenReconciler) reconcileModelConfigStatus(ctx context.Context, modelConfig *v1alpha1.ModelConfig, err error) error {
	var (
		status  metav1.ConditionStatus
		message string
		reason  string
	)
	if err != nil {
		status = metav1.ConditionFalse
		message = err.Error()
		reason = "ModelConfigReconcileFailed"
		reconcileLog.Error(err, "failed to reconcile model config", "modelConfig", modelConfig)
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

func (a *autogenReconciler) ReconcileAutogenTeam(ctx context.Context, req ctrl.Request) error {
	team := &v1alpha1.Team{}
	if err := a.kube.Get(ctx, req.NamespacedName, team); err != nil {
		return fmt.Errorf("failed to get team %s: %v", req.Name, err)
	}

	return a.reconcileTeamStatus(ctx, team, a.reconcileTeams(ctx, team))
}

func (a *autogenReconciler) reconcileTeamStatus(ctx context.Context, team *v1alpha1.Team, err error) error {
	var (
		status  metav1.ConditionStatus
		message string
		reason  string
	)
	if err != nil {
		status = metav1.ConditionFalse
		message = err.Error()
		reconcileLog.Error(err, "failed to reconcile team", "team", team)
		reason = "TeamReconcileFailed"
	} else {
		status = metav1.ConditionTrue
		reason = "TeamReconciled"
	}

	conditionChanged := meta.SetStatusCondition(&team.Status.Conditions, metav1.Condition{
		Type:               v1alpha1.TeamConditionTypeAccepted,
		Status:             status,
		LastTransitionTime: metav1.Now(),
		Reason:             reason,
		Message:            message,
	})

	if conditionChanged || team.Status.ObservedGeneration != team.Generation {
		team.Status.ObservedGeneration = team.Generation
		if err := a.kube.Status().Update(ctx, team); err != nil {
			return fmt.Errorf("failed to update team status: %v", err)
		}
	}

	return nil
}

func (a *autogenReconciler) ReconcileAutogenApiKeySecret(ctx context.Context, req ctrl.Request) error {
	agents, err := a.findAgentsUsingApiKeySecret(ctx, req)
	if err != nil {
		return fmt.Errorf("failed to find agents for secret %s: %v", req.Name, err)
	}

	if err := a.reconcileAgents(ctx, agents...); err != nil {
		return fmt.Errorf("failed to reconcile agents for secret %s: %v", req.Name, err)
	}

	teams, err := a.findTeamsUsingApiKeySecret(ctx, req)
	if err != nil {
		return fmt.Errorf("failed to find teams for api key secret %s: %v", req.Name, err)
	}

	return a.reconcileTeams(ctx, teams...)
}

func (a *autogenReconciler) ReconcileAutogenToolServer(ctx context.Context, req ctrl.Request) error {
	// reconcile the agent team itself
	toolServer := &v1alpha1.ToolServer{}
	if err := a.kube.Get(ctx, req.NamespacedName, toolServer); err != nil {
		// if the tool server is not found, we can ignore it
		if k8s_errors.IsNotFound(err) {
			return nil
		}
		return fmt.Errorf("failed to get tool server %s: %v", req.Name, err)
	}

	serverID, reconcileErr := a.reconcileToolServer(ctx, toolServer)

	// update the tool server status as the agents depend on it
	if err := a.reconcileToolServerStatus(
		ctx,
		toolServer,
		serverID,
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
		return fmt.Errorf("failed to reconcile agents for tool server %s: %v", req.Name, err)
	}

	return nil
}

func (a *autogenReconciler) reconcileToolServerStatus(
	ctx context.Context,
	toolServer *v1alpha1.ToolServer,
	serverID int,
	err error,
) error {
	discoveredTools, discoveryErr := a.getDiscoveredMCPTools(serverID)
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
		reconcileLog.Error(err, "failed to reconcile agent", "agent", toolServer)
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

func (a *autogenReconciler) ReconcileAutogenMemory(ctx context.Context, req ctrl.Request) error {
	memory := &v1alpha1.Memory{}
	if err := a.kube.Get(ctx, req.NamespacedName, memory); err != nil {
		if errors.IsNotFound(err) {
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

func (a *autogenReconciler) handleMemoryDeletion(req ctrl.Request) error {

	// TODO(sbx0r): implement memory deletion

	return nil
}

func (a *autogenReconciler) reconcileMemoryStatus(ctx context.Context, memory *v1alpha1.Memory, err error) error {
	var (
		status  metav1.ConditionStatus
		message string
		reason  string
	)
	if err != nil {
		status = metav1.ConditionFalse
		message = err.Error()
		reason = "MemoryReconcileFailed"
		reconcileLog.Error(err, "failed to reconcile memory", "memory", memory)
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

func (a *autogenReconciler) reconcileTeams(ctx context.Context, teams ...*v1alpha1.Team) error {
	errs := map[types.NamespacedName]error{}
	for _, team := range teams {
		autogenTeam, err := a.autogenTranslator.TranslateGroupChatForTeam(ctx, team)
		if err != nil {
			errs[types.NamespacedName{Name: team.Name, Namespace: team.Namespace}] = fmt.Errorf(
				"failed to translate team %s/%s: %v", team.Namespace, team.Name, err)
			continue
		}
		if err := a.upsertTeam(autogenTeam); err != nil {
			errs[types.NamespacedName{Name: team.Name, Namespace: team.Namespace}] = fmt.Errorf(
				"failed to upsert team %s/%s: %v", team.Namespace, team.Name, err)
			continue
		}
	}

	if len(errs) > 0 {
		return fmt.Errorf("failed to reconcile teams: %v", errs)
	}

	return nil
}

func (a *autogenReconciler) reconcileAgents(ctx context.Context, agents ...*v1alpha1.Agent) error {
	errs := map[types.NamespacedName]error{}
	for _, agent := range agents {
		autogenTeam, err := a.autogenTranslator.TranslateGroupChatForAgent(ctx, agent)
		if err != nil {
			errs[types.NamespacedName{Name: agent.Name, Namespace: agent.Namespace}] = fmt.Errorf(
				"failed to translate agent %s/%s: %v", agent.Namespace, agent.Name, err)
			continue
		}
		if err := a.reconcileA2A(ctx, autogenTeam, agent); err != nil {
			errs[types.NamespacedName{Name: agent.Name, Namespace: agent.Namespace}] = fmt.Errorf(
				"failed to reconcile A2A for agent %s/%s: %v", agent.Namespace, agent.Name, err)
			continue
		}
		if err := a.upsertTeam(autogenTeam); err != nil {
			errs[types.NamespacedName{Name: agent.Name, Namespace: agent.Namespace}] = fmt.Errorf(
				"failed to upsert agent %s/%s: %v", agent.Namespace, agent.Name, err)
			continue
		}
	}

	if len(errs) > 0 {
		return fmt.Errorf("failed to reconcile agents: %v", errs)
	}

	return nil
}

func (a *autogenReconciler) reconcileToolServer(ctx context.Context, server *v1alpha1.ToolServer) (int, error) {
	toolServer, err := a.autogenTranslator.TranslateToolServer(ctx, server)
	if err != nil {
		return 0, fmt.Errorf("failed to translate tool server %s/%s: %v", server.Namespace, server.Name, err)
	}
	serverID, err := a.upsertToolServer(toolServer)
	if err != nil {
		return 0, fmt.Errorf("failed to upsert tool server %s/%s: %v", server.Namespace, server.Name, err)
	}

	return serverID, nil
}

func (a *autogenReconciler) upsertTeam(team *autogen_client.Team) error {
	// lock to prevent races
	a.upsertLock.Lock()
	defer a.upsertLock.Unlock()
	// validate the team
	req := autogen_client.ValidationRequest{
		Component: team.Component,
	}
	resp, err := a.autogenClient.Validate(&req)
	if err != nil {
		return fmt.Errorf("failed to validate team %s: %v", team.Component.Label, err)
	}
	if !resp.IsValid {
		return fmt.Errorf("team %s is invalid: %v", team.Component.Label, resp.ErrorMsg())
	}

	// delete if team exists
	existingTeam, err := a.autogenClient.GetTeam(team.Component.Label, common.GetGlobalUserID())
	if err != nil {
		return fmt.Errorf("failed to get existing team %s: %v", team.Component.Label, err)
	}
	if existingTeam != nil {
		team.Id = existingTeam.Id
	}

	return a.autogenClient.CreateTeam(team)
}

func (a *autogenReconciler) upsertToolServer(toolServer *autogen_client.ToolServer) (int, error) {
	// lock to prevent races
	a.upsertLock.Lock()
	defer a.upsertLock.Unlock()

	// delete if toolServer exists
	existingToolServer, err := a.autogenClient.GetToolServerByLabel(toolServer.Component.Label, common.GetGlobalUserID())
	if err != nil && !strings.Contains(err.Error(), "not found") {
		return 0, fmt.Errorf("failed to get existing toolServer %s: %v", toolServer.Component.Label, err)
	}
	if existingToolServer != nil {
		toolServer.Id = existingToolServer.Id
		err = a.autogenClient.UpdateToolServer(toolServer, common.GetGlobalUserID())
		if err != nil {
			return 0, fmt.Errorf("failed to delete existing toolServer %s: %v", toolServer.Component.Label, err)
		}
	} else {
		existingToolServer, err = a.autogenClient.CreateToolServer(toolServer, common.GetGlobalUserID())
		if err != nil {
			return 0, fmt.Errorf("failed to create toolServer %s: %v", toolServer.Component.Label, err)
		}
		existingToolServer, err = a.autogenClient.GetToolServerByLabel(toolServer.Component.Label, common.GetGlobalUserID())
		if err != nil {
			return 0, fmt.Errorf("failed to get existing toolServer %s: %v", toolServer.Component.Label, err)
		}
	}

	err = a.autogenClient.RefreshToolServer(existingToolServer.Id, common.GetGlobalUserID())
	if err != nil {
		return 0, fmt.Errorf("failed to refresh toolServer %s: %v", toolServer.Component.Label, err)
	}

	return existingToolServer.Id, nil
}

func (a *autogenReconciler) findAgentsUsingModel(ctx context.Context, req ctrl.Request) ([]*v1alpha1.Agent, error) {
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
		agentNamespaced, err := common.ParseRefString(agent.Spec.ModelConfig, agent.Namespace);

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

func (a *autogenReconciler) findAgentsUsingApiKeySecret(ctx context.Context, req ctrl.Request) ([]*v1alpha1.Agent, error) {
	var modelsList v1alpha1.ModelConfigList
	if err := a.kube.List(
		ctx,
		&modelsList,
	); err != nil {
		return nil, fmt.Errorf("failed to list ModelConfigs: %v", err)
	}

	var models []string
	for _, model := range modelsList.Items {
		secretNamespaced, err := common.ParseRefString(model.Spec.APIKeySecretRef, model.Namespace)

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
			key := common.GetObjectRef(agent)
			if !uniqueAgents[key] {
				uniqueAgents[key] = true
				agents = append(agents, agent)
			}
		}
	}

	return agents, nil
}

func (a *autogenReconciler) findAgentsUsingMemory(ctx context.Context, req ctrl.Request) ([]*v1alpha1.Agent, error) {
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
			memoryNamespaced, err := common.ParseRefString(memory, agent.Namespace);

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

func (a *autogenReconciler) findTeamsUsingAgent(ctx context.Context, req ctrl.Request) ([]*v1alpha1.Team, error) {
	var teamsList v1alpha1.TeamList
	if err := a.kube.List(
		ctx,
		&teamsList,
	); err != nil {
		return nil, fmt.Errorf("failed to list teams: %v", err)
	}

	var teams []*v1alpha1.Team
	for i := range teamsList.Items {
		team := &teamsList.Items[i]
		for _, participant := range team.Spec.Participants {
			participantNamespaced, err := common.ParseRefString(participant, team.Namespace);

			if err != nil {
				reconcileLog.Error(err, "failed to parse Team participant",
					"errorDetails", err.Error(),
				)
				continue
			}

			if participantNamespaced == req.NamespacedName {
				teams = append(teams, team)
				break
			}
		}
	}

	return teams, nil
}

func (a *autogenReconciler) findTeamsUsingModel(ctx context.Context, req ctrl.Request) ([]*v1alpha1.Team, error) {
	var teamsList v1alpha1.TeamList
	if err := a.kube.List(
		ctx,
		&teamsList,
	); err != nil {
		return nil, fmt.Errorf("failed to list Teams: %v", err)
	}

	var teams []*v1alpha1.Team
	for i := range teamsList.Items {
		team := &teamsList.Items[i]
		modelNamespaced, err := common.ParseRefString(team.Spec.ModelConfig, team.Namespace)

		if err != nil {
			reconcileLog.Error(err, "failed to parse Team ModelConfig",
				"errorDetails", err.Error(),
			)
			continue
		}

		if modelNamespaced == req.NamespacedName {
			teams = append(teams, team)
		}
	}

	return teams, nil
}

func (a *autogenReconciler) findTeamsUsingApiKeySecret(ctx context.Context, req ctrl.Request) ([]*v1alpha1.Team, error) {
	var modelsList v1alpha1.ModelConfigList
	if err := a.kube.List(
		ctx,
		&modelsList,
	); err != nil {
		return nil, fmt.Errorf("failed to list ModelConfigs: %v", err)
	}

	var models []string
	for _, model := range modelsList.Items {
		secretNamespaced, err := common.ParseRefString(model.Spec.APIKeySecretRef, model.Namespace)

		if err != nil {
			switch e := err.(type) {
			case *common.EmptyReferenceError:
				reconcileLog.V(4).Info("ModelConfig has empty APIKeySecretRef, skipping",
					"model", model.Name,
					"namespace", model.Namespace,
				)
            default:
				reconcileLog.Error(err, "failed to parse ModelConfig APIKeySecretRef",
					"errorDetails", e.Error(),
					"model", model.Name,
					"namespace", model.Namespace,
				)
			}
			continue
		}

		if secretNamespaced == req.NamespacedName {
			models = append(models, model.Name)
		}
	}

	var teams []*v1alpha1.Team
	uniqueTeams := make(map[string]bool)

	for _, modelName := range models {
		teamsUsingModel, err := a.findTeamsUsingModel(ctx, ctrl.Request{
			NamespacedName: types.NamespacedName{
				Namespace: req.Namespace,
				Name:      modelName,
			},
		})
		if err != nil {
			return nil, fmt.Errorf("failed to find teams for model %s: %v", modelName, err)
		}

		for _, team := range teamsUsingModel {
			key := common.GetObjectRef(team)
			if !uniqueTeams[key] {
				uniqueTeams[key] = true
				teams = append(teams, team)
			}
		}
	}

	return teams, nil
}

func (a *autogenReconciler) findAgentsUsingToolServer(ctx context.Context, req ctrl.Request) ([]*v1alpha1.Agent, error) {
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

			toolServerNamespaced, err := common.ParseRefString(tool.McpServer.ToolServer, agent.Namespace)
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

func (a *autogenReconciler) getDiscoveredMCPTools(serverID int) ([]*v1alpha1.MCPTool, error) {
	allTools, err := a.autogenClient.ListTools(common.GetGlobalUserID())
	if err != nil {
		return nil, err
	}

	var discoveredTools []*v1alpha1.MCPTool
	for _, tool := range allTools {
		if tool.ServerID != nil && *tool.ServerID == serverID {
			mcpTool, err := convertTool(tool)
			if err != nil {
				return nil, fmt.Errorf("failed to convert tool: %v", err)
			}
			discoveredTools = append(discoveredTools, mcpTool)
		}
	}

	return discoveredTools, nil
}

func (a *autogenReconciler) reconcileA2A(
	ctx context.Context,
	team *autogen_client.Team,
	agent *v1alpha1.Agent,
) error {
	return a.a2aReconciler.ReconcileAutogenAgent(ctx, agent, team)
}

func convertTool(tool *autogen_client.Tool) (*v1alpha1.MCPTool, error) {
	if tool.Component == nil || tool.Component.Config == nil {
		return nil, fmt.Errorf("missing component or config")
	}
	config := tool.Component.Config
	var mcpToolConfig api.MCPToolConfig
	if err := unmarshalFromMap(config, &mcpToolConfig); err != nil {
		return nil, fmt.Errorf("failed to unmarshal tool config: %v", err)
	}
	component, err := convertComponentToApiType(tool.Component)
	if err != nil {
		return nil, fmt.Errorf("failed to convert component: %v", err)
	}

	return &v1alpha1.MCPTool{
		Name:      mcpToolConfig.Tool.Name,
		Component: component,
	}, nil
	//
	//inputSchema, err := convertToAnyType(mcpToolConfig.Tool.InputSchema)
	//if err != nil {
	//	return nil, fmt.Errorf("failed to convert input schema: %v", err)
	//}
	//
	//serverParams := mcpToolConfig.ServerParams
	//if serverParams == nil {
	//	return nil, fmt.Errorf("missing server params")
	//}
	//serverParamsMap, ok := serverParams.(map[string]interface{})
	//if !ok {
	//	return nil, fmt.Errorf("invalid server params")
	//}
	//
	//// if serevr params contains "command", parse it as Stdio, else look for "url" and parse it as Sse
	//_, hasCommand := serverParamsMap["command"]
	//_, hasUrl := serverParamsMap["url"]
	//
	//var (
	//	stdio *v1alpha1.StdioMcpServerConfig
	//	sse   *v1alpha1.SseMcpServerConfig
	//)
	//switch {
	//case hasCommand:
	//	stdioConfig := &api.StdioMcpServerConfig{}
	//	if err := unmarshalFromMap(serverParamsMap, stdioConfig); err != nil {
	//		return nil, fmt.Errorf("failed to unmarshal stdio config: %v", err)
	//	}
	//	stderr, ok := stdioConfig.Stderr.(string)
	//	if !ok {
	//		stderr = ""
	//	}
	//
	//	stdio = &v1alpha1.StdioMcpServerConfig{
	//		Command: stdioConfig.Command,
	//		Args:    stdioConfig.Args,
	//		Env:     stdioConfig.Env,
	//		Stderr:  stderr,
	//		Cwd:     stdioConfig.Cwd,
	//	}
	//case hasUrl:
	//	sseConfig := &api.SseMcpServerConfig{}
	//	if err := unmarshalFromMap(serverParamsMap, sseConfig); err != nil {
	//		return nil, fmt.Errorf("failed to unmarshal sse config: %v", err)
	//	}
	//
	//	sse = &v1alpha1.SseMcpServerConfig{
	//		URL:            sseConfig.URL,
	//		Headers:        sseConfig.Headers,
	//		Timeout:        convertTimeout(sseConfig.Timeout),
	//		SseReadTimeout: convertTimeout(sseConfig.SseReadTimeout),
	//	}
	//}
	//
	//return &v1alpha1.MCPTool{
	//	Name:        mcpToolConfig.Tool.Name,
	//	Description: mcpToolConfig.Tool.Description,
	//	InputSchema: inputSchema,
	//	ServerParams: v1alpha1.MCPToolServerParams{
	//		Stdio: stdio,
	//		Sse:   sse,
	//	},
	//}, nil
}

func convertComponentToApiType(component *api.Component) (v1alpha1.Component, error) {
	anyConfig, err := convertMapToAnytype(component.Config)
	if err != nil {
		return v1alpha1.Component{}, err
	}
	return v1alpha1.Component{
		Provider:         component.Provider,
		ComponentType:    component.ComponentType,
		Version:          component.Version,
		ComponentVersion: component.ComponentVersion,
		Description:      component.Description,
		Label:            component.Label,
		Config:           anyConfig,
	}, nil
}

func convertMapToAnytype(m map[string]interface{}) (map[string]v1alpha1.AnyType, error) {
	anyConfig := make(map[string]v1alpha1.AnyType)
	for k, v := range m {
		b, err := json.Marshal(v)
		if err != nil {
			return nil, err
		}
		anyConfig[k] = v1alpha1.AnyType{
			RawMessage: b,
		}
	}
	return anyConfig, nil
}

func unmarshalFromMap(m map[string]interface{}, v interface{}) error {
	b, err := json.Marshal(m)
	if err != nil {
		return err
	}
	return json.Unmarshal(b, v)
}
