package a2agateway

import (
	"context"
	"crypto/tls"
	"fmt"
	"net/http"
	"net/url"

	a2atype "github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/a2aproject/a2a-go/v2/a2aclient"
	"github.com/a2aproject/a2a-go/v2/a2aext"
	a2agrpc "github.com/a2aproject/a2a-go/v2/a2agrpc/v1"
	apiv1alpha1 "github.com/kagent-dev/kagent/go/api/gen/kagent/api/v1alpha1"
	"github.com/kagent-dev/kagent/go/core/pkg/auth"
	"go.opentelemetry.io/otel/propagation"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/credentials/insecure"
)

// RuntimeDialer connects public gateway calls to the single root Actor used by
// the current v0 AgentInstance implementation. Replacing this component with a
// member-store-backed dialer is sufficient when runtime topology becomes
// explicit; the gateway handler does not depend on Actor naming or Atenet.
type RuntimeDialer struct {
	target        string
	transport     credentials.TransportCredentials
	authenticator auth.AuthProvider
}

// NewRuntimeDialer configures private A2A gRPC calls through Substrate's
// shared Atenet router; the selected Actor is carried only in :authority.
func NewRuntimeDialer(routerURL string, authenticator auth.AuthProvider) (*RuntimeDialer, error) {
	router, err := url.Parse(routerURL)
	if err != nil {
		return nil, fmt.Errorf("parse Atenet router URL %q: %w", routerURL, err)
	}
	if router.Host == "" {
		return nil, fmt.Errorf("atenet router URL %q must include a host", routerURL)
	}
	if authenticator == nil {
		return nil, fmt.Errorf("atenet runtime authentication is not configured")
	}
	var transport credentials.TransportCredentials
	switch router.Scheme {
	case "http":
		transport = insecure.NewCredentials()
	case "https":
		transport = credentials.NewTLS(&tls.Config{MinVersion: tls.VersionTLS12, ServerName: router.Hostname()})
	default:
		return nil, fmt.Errorf("atenet router URL %q must use http or https", routerURL)
	}
	return &RuntimeDialer{target: router.Host, transport: transport, authenticator: authenticator}, nil
}

func (d *RuntimeDialer) Dial(ctx context.Context, instance *apiv1alpha1.AgentInstance) (*a2aclient.Client, error) {
	if instance.GetA2AAuthority() == "" {
		return nil, fmt.Errorf("runtime authority is empty")
	}
	// ponytail: scope one connection to one public RPC until gateway traffic
	// justifies a lifecycle-aware per-instance connection pool.
	return a2aclient.NewFromEndpoints(ctx, []*a2atype.AgentInterface{{
		URL:             d.target,
		ProtocolBinding: a2atype.TransportProtocolGRPC,
		ProtocolVersion: a2atype.Version,
	}},
		a2agrpc.WithGRPCTransport(
			grpc.WithTransportCredentials(d.transport),
			grpc.WithAuthority(instance.GetA2AAuthority()),
		),
		a2aclient.WithCallInterceptors(
			a2aext.NewClientPropagator(nil),
			&upstreamAuthInterceptor{authenticator: d.authenticator, instance: instance},
		),
	)
}

// upstreamAuthInterceptor mirrors the current gateway's per-request auth
// forwarding. ServiceParams make the resulting headers transport-neutral: the
// A2A gRPC transport carries them as metadata to the private runtime.
type upstreamAuthInterceptor struct {
	a2aclient.PassthroughInterceptor
	authenticator auth.AuthProvider
	instance      *apiv1alpha1.AgentInstance
}

func (u *upstreamAuthInterceptor) Before(ctx context.Context, req *a2aclient.Request) (context.Context, any, error) {
	httpRequest, err := http.NewRequestWithContext(ctx, http.MethodPost, "http://"+req.BaseURL, nil)
	if err != nil {
		return ctx, nil, err
	}
	if session, ok := auth.AuthSessionFrom(ctx); ok {
		principal := auth.Principal{Agent: auth.Agent{ID: u.instance.GetNamespace() + "/" + u.instance.GetId()}}
		if err := u.authenticator.UpstreamAuth(httpRequest, session, principal); err != nil {
			return ctx, nil, err
		}
	}
	propagation.TraceContext{}.Inject(ctx, propagation.HeaderCarrier(httpRequest.Header))
	for key, values := range httpRequest.Header {
		for _, value := range values {
			req.ServiceParams.Append(key, value)
		}
	}
	return ctx, nil, nil
}
