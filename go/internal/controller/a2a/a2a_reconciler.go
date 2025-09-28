package a2a

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"os"
	"time"

	"github.com/kagent-dev/kagent/go/api/v1alpha2"
	"github.com/kagent-dev/kagent/go/internal/a2a"
	authimpl "github.com/kagent-dev/kagent/go/internal/httpserver/auth"
	common "github.com/kagent-dev/kagent/go/internal/utils"
	"github.com/kagent-dev/kagent/go/pkg/auth"
	"k8s.io/apimachinery/pkg/types"
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

type ClientOptions struct {
	StreamingMaxBufSize     int
	StreamingInitialBufSize int
	Timeout                 time.Duration
}

type a2aReconciler struct {
	a2aHandler    a2a.A2AHandlerMux
	a2aBaseUrl    string
	authenticator auth.AuthProvider
	clientOptions ClientOptions
}

func NewReconciler(
	a2aHandler a2a.A2AHandlerMux,
	a2aBaseUrl string,
	clientOptions ClientOptions,
	authenticator auth.AuthProvider,
) A2AReconciler {
	return &a2aReconciler{
		a2aHandler:    a2aHandler,
		a2aBaseUrl:    a2aBaseUrl,
		clientOptions: clientOptions,
		authenticator: authenticator,
	}
}

func (a *a2aReconciler) ReconcileAgent(
	ctx context.Context,
	agent *v1alpha2.Agent,
	card server.AgentCard,
) error {
	agentRef := common.GetObjectRef(agent)
	agentNns := types.NamespacedName{Namespace: agent.GetNamespace(), Name: agent.GetName()}

	client, err := a2aclient.NewA2AClient(card.URL,
		a2aclient.WithTimeout(a.clientOptions.Timeout),
		a2aclient.WithBuffer(a.clientOptions.StreamingInitialBufSize, a.clientOptions.StreamingMaxBufSize),
		debugOpt(),
		a2aclient.WithHTTPReqHandler(authimpl.A2ARequestHandler(a.authenticator, agentNns)),
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
