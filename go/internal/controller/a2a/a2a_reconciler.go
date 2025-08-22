package a2a

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"os"

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
		debugOpt(),
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

func debugOpt() a2aclient.Option {
	debugAddr := os.Getenv("KAGENT_A2A_DEBUG_ADDR")
	if debugAddr != "" {
		client := new(http.Client)
		client.Transport = &http.Transport{
			DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
				var zeroDialer net.Dialer
				return zeroDialer.DialContext(ctx, network, debugAddr)
			},
		}
		return a2aclient.WithHTTPClient(client)
	} else {
		return func(*a2aclient.A2AClient) {}
	}
}
