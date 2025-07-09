package a2a

import (
	"context"

	"github.com/kagent-dev/kagent/go/controller/api/v1alpha1"
	"github.com/kagent-dev/kagent/go/internal/a2a"
	autogen_client "github.com/kagent-dev/kagent/go/internal/autogen/client"
	"github.com/kagent-dev/kagent/go/internal/database"
	common "github.com/kagent-dev/kagent/go/internal/utils"
	ctrl "sigs.k8s.io/controller-runtime"
)

var (
	reconcileLog = ctrl.Log.WithName("a2a_reconcile")
)

type A2AReconciler interface {
	ReconcileAutogenAgent(
		ctx context.Context,
		agent *v1alpha1.Agent,
		autogenTeam *database.Agent,
	) error

	ReconcileAutogenAgentDeletion(
		agentRef string,
	)
}

type a2aReconciler struct {
	a2aTranslator AutogenA2ATranslator
	autogenClient autogen_client.Client
	a2aHandler    a2a.A2AHandlerMux
}

func NewAutogenReconciler(
	autogenClient autogen_client.Client,
	a2aHandler a2a.A2AHandlerMux,
	a2aBaseUrl string,
	dbService database.Client,
) A2AReconciler {
	return &a2aReconciler{
		a2aTranslator: NewAutogenA2ATranslator(a2aBaseUrl, autogenClient, dbService),
		autogenClient: autogenClient,
		a2aHandler:    a2aHandler,
	}
}

func (a *a2aReconciler) ReconcileAutogenAgent(
	ctx context.Context,
	agent *v1alpha1.Agent,
	autogenTeam *database.Agent,
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
