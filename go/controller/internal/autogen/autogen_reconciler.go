package autogen

import (
	"context"
	"fmt"
	"sync"

	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	autogen_client "github.com/kagent-dev/kagent/go/autogen/client"
	"github.com/kagent-dev/kagent/go/controller/api/v1alpha1"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

var (
	reconcileLog = ctrl.Log.WithName("reconcile")
)

type AutogenReconciler interface {
	ReconcileAutogenAgent(ctx context.Context, req ctrl.Request) error
	ReconcileAutogenModelConfig(ctx context.Context, req ctrl.Request) error
	ReconcileAutogenTeam(ctx context.Context, req ctrl.Request) error
	ReconcileAutogenApiKeySecret(ctx context.Context, req ctrl.Request) error
}

type autogenReconciler struct {
	translator ApiTranslator

	kube          client.Client
	autogenClient *autogen_client.Client

	defaultModelConfig types.NamespacedName
	upsertLock         sync.Mutex
}

func NewAutogenReconciler(
	translator ApiTranslator,
	kube client.Client,
	autogenClient *autogen_client.Client,
	defaultModelConfig types.NamespacedName,
) AutogenReconciler {
	return &autogenReconciler{
		translator:         translator,
		kube:               kube,
		autogenClient:      autogenClient,
		defaultModelConfig: defaultModelConfig,
	}
}

func (a *autogenReconciler) ReconcileAutogenAgent(ctx context.Context, req ctrl.Request) error {
	// reconcile the agent team itself
	agent := &v1alpha1.Agent{}
	if err := a.kube.Get(ctx, req.NamespacedName, agent); err != nil {
		return fmt.Errorf("failed to get agent %s: %v", req.Name, err)
	}
	if err := a.reconcileAgents(ctx, agent); err != nil {
		return fmt.Errorf("failed to reconcile agent %s: %v", req.Name, err)
	}

	// find and reconcile all teams which use this agent
	teams, err := a.findTeamsUsingAgent(ctx, req)
	if err != nil {
		return fmt.Errorf("failed to find teams for agent %s: %v", req.Name, err)
	}

	return a.reconcileAgentStatus(
		ctx,
		agent,
		a.reconcileTeams(ctx, teams...),
	)
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

func (a *autogenReconciler) reconcileTeams(ctx context.Context, teams ...*v1alpha1.Team) error {
	errs := map[types.NamespacedName]error{}
	for _, team := range teams {
		autogenTeam, err := a.translator.TranslateGroupChatForTeam(ctx, team)
		if err != nil {
			errs[types.NamespacedName{Name: team.Name, Namespace: team.Namespace}] = fmt.Errorf("failed to translate team %s: %v", team.Name, err)
			continue
		}
		if err := a.upsertTeam(autogenTeam); err != nil {
			errs[types.NamespacedName{Name: team.Name, Namespace: team.Namespace}] = fmt.Errorf("failed to upsert team %s: %v", team.Name, err)
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
		autogenTeam, err := a.translator.TranslateGroupChatForAgent(ctx, agent)
		if err != nil {
			errs[types.NamespacedName{Name: agent.Name, Namespace: agent.Namespace}] = fmt.Errorf("failed to translate agent %s: %v", agent.Name, err)
			continue
		}
		if err := a.upsertTeam(autogenTeam); err != nil {
			errs[types.NamespacedName{Name: agent.Name, Namespace: agent.Namespace}] = fmt.Errorf("failed to upsert agent %s: %v", agent.Name, err)
			continue
		}
	}

	if len(errs) > 0 {
		return fmt.Errorf("failed to reconcile agents: %v", errs)
	}

	return nil
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
		return fmt.Errorf("failed to validate team %s: %v", *team.Component.Label, err)
	}
	if !resp.IsValid {
		return fmt.Errorf("team %s is invalid: %v", *team.Component.Label, resp.ErrorMsg())
	}

	// delete if team exists
	existingTeam, err := a.autogenClient.GetTeam(*team.Component.Label, GlobalUserID)
	if err != nil {
		return fmt.Errorf("failed to get existing team %s: %v", *team.Component.Label, err)
	}
	if existingTeam != nil {
		err = a.autogenClient.DeleteTeam(existingTeam.Id, GlobalUserID)
		if err != nil {
			return fmt.Errorf("failed to delete existing team %s: %v", *team.Component.Label, err)
		}
		team.Id = existingTeam.Id
	}

	return a.autogenClient.CreateTeam(team)
}

func (a *autogenReconciler) findAgentsUsingModel(ctx context.Context, req ctrl.Request) ([]*v1alpha1.Agent, error) {
	var agentsList v1alpha1.AgentList
	if err := a.kube.List(
		ctx,
		&agentsList,
		client.InNamespace(req.Namespace),
	); err != nil {
		return nil, fmt.Errorf("failed to list agents: %v", err)
	}

	var agents []*v1alpha1.Agent
	for i := range agentsList.Items {
		agent := &agentsList.Items[i]
		if agent.Spec.ModelConfigRef == req.Name {
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
		client.InNamespace(req.Namespace),
	); err != nil {
		return nil, fmt.Errorf("failed to list model configs: %v", err)
	}

	var models []string
	for _, model := range modelsList.Items {
		if model.Spec.APIKeySecretName == req.Name {
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
			key := fmt.Sprintf("%s/%s", agent.Namespace, agent.Name)
			if !uniqueAgents[key] {
				uniqueAgents[key] = true
				agents = append(agents, agent)
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
		client.InNamespace(req.Namespace),
	); err != nil {
		return nil, fmt.Errorf("failed to list teams: %v", err)
	}

	var teams []*v1alpha1.Team
	for i := range teamsList.Items {
		team := &teamsList.Items[i]
		for _, participant := range team.Spec.Participants {
			if participant == req.Name {
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
		client.InNamespace(req.Namespace),
	); err != nil {
		return nil, fmt.Errorf("failed to list teams: %v", err)
	}

	var teams []*v1alpha1.Team
	for i := range teamsList.Items {
		team := &teamsList.Items[i]
		if team.Spec.ModelConfig == req.Name {
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
		client.InNamespace(req.Namespace),
	); err != nil {
		return nil, fmt.Errorf("failed to list model configs: %v", err)
	}

	var models []string
	for _, model := range modelsList.Items {
		if model.Spec.APIKeySecretName == req.Name {
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
			key := fmt.Sprintf("%s/%s", team.Namespace, team.Name)
			if !uniqueTeams[key] {
				uniqueTeams[key] = true
				teams = append(teams, team)
			}
		}
	}

	return teams, nil
}
