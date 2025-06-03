package a2a

import (
	"context"

	autogen_client "github.com/kagent-dev/kagent/go/autogen/client"
	"github.com/kagent-dev/kagent/go/controller/api/v1alpha1"
	common "github.com/kagent-dev/kagent/go/controller/internal/utils"
	ctrl "sigs.k8s.io/controller-runtime"
)

var (
	reconcileLog = ctrl.Log.WithName("a2a_reconcile")
)

type A2AReconciler interface {
	ReconcileAutogenAgent(
		ctx context.Context,
		agent *v1alpha1.Agent,
		autogenTeam *autogen_client.Team,
	) error

	ReconcileAutogenAgentDeletion(
		agentRef string,
	)
}

type a2aReconciler struct {
	a2aTranslator AutogenA2ATranslator
	autogenClient autogen_client.Client
	a2aHandler    A2AHandlerMux
}

func NewAutogenReconciler(
	autogenClient autogen_client.Client,
	a2aHandler A2AHandlerMux,
	a2aBaseUrl string,
) A2AReconciler {
	return &a2aReconciler{
		a2aTranslator: NewAutogenA2ATranslator(a2aBaseUrl, autogenClient),
		autogenClient: autogenClient,
		a2aHandler:    a2aHandler,
	}
}

func (a *a2aReconciler) ReconcileAutogenAgent(
	ctx context.Context,
	agent *v1alpha1.Agent,
	autogenTeam *autogen_client.Team,
) error {
	params, err := a.a2aTranslator.TranslateHandlerForAgent(ctx, agent, autogenTeam)
	if err != nil {
		return err
	}

	agentRef := common.GetObjectRef(agent)
	if params == nil {
		reconcileLog.Info("No a2a handler found for agent, a2a will be disabled", "agent", agentRef)
		return nil
	}

	return a.a2aHandler.SetAgentHandler(
		agentRef,
		params,
	)
}

func (a *a2aReconciler) ReconcileAutogenAgentDeletion(
	agentRef string,
) {
	a.a2aHandler.RemoveAgentHandler(agentRef)
}
