package a2agateway

import (
	"context"
	"fmt"
	"net/http"

	a2atype "github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/a2aproject/a2a-go/v2/a2aclient"
	"github.com/a2aproject/a2a-go/v2/a2aext"
	a2agrpc "github.com/a2aproject/a2a-go/v2/a2agrpc/v1"
	"github.com/kagent-dev/kagent/go/api/client"
	apiv1alpha1 "github.com/kagent-dev/kagent/go/api/gen/kagent/api/v1alpha1"
	"github.com/kagent-dev/kagent/go/core/internal/substrate"
	"github.com/kagent-dev/kagent/go/core/pkg/auth"
	"go.opentelemetry.io/contrib/instrumentation/google.golang.org/grpc/otelgrpc"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
)

// RuntimeDialer connects public gateway calls to the single root Actor used by
// the current v0 Session implementation. Replacing this component with a
// member-store-backed dialer is sufficient when runtime topology becomes
// explicit; the gateway handler does not depend on Actor naming or Atenet.
type RuntimeDialer struct {
	target        string
	transport     credentials.TransportCredentials
	authenticator auth.AuthProvider
}

// NewRuntimeDialer configures private A2A gRPC calls through Substrate's
// shared Atenet router; ate-target-actor selects the Actor.
func NewRuntimeDialer(router substrate.Router, authenticator auth.AuthProvider) (*RuntimeDialer, error) {
	target, transport, err := router.Transport()
	if err != nil {
		return nil, err
	}
	if authenticator == nil {
		return nil, fmt.Errorf("atenet runtime authentication is not configured")
	}
	return &RuntimeDialer{target: target, transport: transport, authenticator: authenticator}, nil
}

func (d *RuntimeDialer) Dial(ctx context.Context, session *apiv1alpha1.Session) (*a2aclient.Client, error) {
	targetActor, err := substrate.ActorTargetFromHost(session.GetA2AAuthority())
	if err != nil {
		return nil, err
	}
	return a2aclient.NewFromEndpoints(ctx, []*a2atype.AgentInterface{{
		URL:             d.target,
		ProtocolBinding: a2atype.TransportProtocolGRPC,
		ProtocolVersion: a2atype.Version,
	}},
		a2agrpc.WithGRPCTransport(
			grpc.WithTransportCredentials(d.transport),
			grpc.WithStatsHandler(otelgrpc.NewClientHandler()),
			grpc.WithDefaultCallOptions(grpc.MaxCallRecvMsgSize(client.DefaultGRPCMaxMessageSize), grpc.MaxCallSendMsgSize(client.DefaultGRPCMaxMessageSize)),
		),
		a2aclient.WithCallInterceptors(
			a2aext.NewClientPropagator(nil),
			&upstreamAuthInterceptor{authenticator: d.authenticator, session: session, targetActor: targetActor},
		),
	)
}

// upstreamAuthInterceptor mirrors the current gateway's per-request auth
// forwarding. ServiceParams make the resulting headers transport-neutral: the
// A2A gRPC transport carries them as metadata to the private runtime.
type upstreamAuthInterceptor struct {
	a2aclient.PassthroughInterceptor
	authenticator auth.AuthProvider
	session       *apiv1alpha1.Session
	targetActor   string
}

func (u *upstreamAuthInterceptor) Before(ctx context.Context, req *a2aclient.Request) (context.Context, any, error) {
	httpRequest, err := http.NewRequestWithContext(ctx, http.MethodPost, "http://"+req.BaseURL, nil)
	if err != nil {
		return ctx, nil, err
	}
	if session, ok := auth.AuthSessionFrom(ctx); ok {
		principal := auth.Principal{Agent: auth.Agent{ID: u.session.GetId()}}
		if err := u.authenticator.UpstreamAuth(httpRequest, session, principal); err != nil {
			return ctx, nil, err
		}
	}
	for key, values := range httpRequest.Header {
		for _, value := range values {
			req.ServiceParams.Append(key, value)
		}
	}
	req.ServiceParams["ate-target-actor"] = []string{u.targetActor}
	return ctx, nil, nil
}
