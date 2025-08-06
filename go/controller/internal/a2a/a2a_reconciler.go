package a2a

import (
	"context"
	"fmt"

	"github.com/kagent-dev/kagent/go/controller/api/v1alpha2"
	"github.com/kagent-dev/kagent/go/internal/a2a"
	"github.com/kagent-dev/kagent/go/internal/adk"
	common "github.com/kagent-dev/kagent/go/internal/utils"
	ctrl "sigs.k8s.io/controller-runtime"
	a2aclient "trpc.group/trpc-go/trpc-a2a-go/client"
)

var (
	reconcileLog = ctrl.Log.WithName("a2a_reconcile")
)

type A2AReconciler interface {
	ReconcileAgent(
		ctx context.Context,
		agent *v1alpha2.Agent,
		adkConfig *adk.AgentConfig,
	) error

	ReconcileAgentDeletion(
		agentRef string,
	)
}

type a2aReconciler struct {
	a2aHandler a2a.A2AHandlerMux
	a2aBaseUrl string

	streamingMaxBufSize     int
	streamingInitialBufSize int
}

func NewReconciler(
	a2aHandler a2a.A2AHandlerMux,
	a2aBaseUrl string,
	streamingMaxBufSize int,
	streamingInitialBufSize int,
) A2AReconciler {
	return &a2aReconciler{
		a2aHandler:              a2aHandler,
		a2aBaseUrl:              a2aBaseUrl,
		streamingMaxBufSize:     streamingMaxBufSize,
		streamingInitialBufSize: streamingInitialBufSize,
	}
}

func (a *a2aReconciler) ReconcileAgent(
	ctx context.Context,
	agent *v1alpha2.Agent,
	adkConfig *adk.AgentConfig,
) error {
	cardCopy := adkConfig.AgentCard
	// Modify card for kagent proxy
	agentRef := common.GetObjectRef(agent)
	cardCopy.URL = fmt.Sprintf("%s/%s/", a.a2aBaseUrl, agentRef)

	client, err := a2aclient.NewA2AClient(adkConfig.AgentCard.URL, a2aclient.WithBuffer(a.streamingInitialBufSize, a.streamingMaxBufSize))
	if err != nil {
		return err
	}

	return a.a2aHandler.SetAgentHandler(
		agentRef,
		client,
		cardCopy,
	)
}

func (a *a2aReconciler) ReconcileAgentDeletion(
	agentRef string,
) {
	a.a2aHandler.RemoveAgentHandler(agentRef)
}
