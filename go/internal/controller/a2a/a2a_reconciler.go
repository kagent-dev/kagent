package a2a

import (
	"context"
	"fmt"

	"github.com/kagent-dev/kagent/go/api/v1alpha2"
	"github.com/kagent-dev/kagent/go/internal/a2a"
	"github.com/kagent-dev/kagent/go/internal/httpserver/auth"
	common "github.com/kagent-dev/kagent/go/internal/utils"
	a2aclient "trpc.group/trpc-go/trpc-a2a-go/client"
	"trpc.group/trpc-go/trpc-a2a-go/server"
)

type A2AReconciler interface {
	ReconcileAgent(
		ctx context.Context,
		agent *v1alpha2.Agent,
		card server.AgentCard,
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
	authenticator           auth.AuthProvider
}

func NewReconciler(
	a2aHandler a2a.A2AHandlerMux,
	a2aBaseUrl string,
	streamingMaxBufSize int,
	streamingInitialBufSize int,
	authenticator auth.AuthProvider,
) A2AReconciler {
	return &a2aReconciler{
		a2aHandler:              a2aHandler,
		a2aBaseUrl:              a2aBaseUrl,
		streamingMaxBufSize:     streamingMaxBufSize,
		streamingInitialBufSize: streamingInitialBufSize,
		authenticator:           authenticator,
	}
}

func (a *a2aReconciler) ReconcileAgent(
	ctx context.Context,
	agent *v1alpha2.Agent,
	card server.AgentCard,
) error {
	agentRef := common.GetObjectRef(agent)

	client, err := a2aclient.NewA2AClient(card.URL,
		a2aclient.WithBuffer(a.streamingInitialBufSize, a.streamingMaxBufSize),
		a2aclient.WithHTTPReqHandler(auth.A2ARequestHandler(a.authenticator)),
	)
	if err != nil {
		return err
	}

	// Modify card for kagent proxy
	cardCopy := card
	cardCopy.URL = fmt.Sprintf("%s/%s/", a.a2aBaseUrl, agentRef)

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
