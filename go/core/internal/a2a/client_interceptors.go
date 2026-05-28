package a2a

import (
	"context"
	"net/http"

	"github.com/a2aproject/a2a-go/v2/a2aclient"
	"github.com/kagent-dev/kagent/go/core/pkg/auth"
	"go.opentelemetry.io/otel/propagation"
	"k8s.io/apimachinery/pkg/types"
)

// staticHeadersInterceptor injects agent-level static headers (e.g. API keys, tenant IDs)
// into every outgoing A2A call. Headers are fixed at construction time so they are never
// re-resolved per request, which makes the interceptor safe for concurrent calls.
// Currently this is only used for testing in invoke_api_test.go
type staticHeadersInterceptor struct {
	a2aclient.PassthroughInterceptor
	headers map[string]string
}

func NewStaticHeadersInterceptor(headers map[string]string) a2aclient.CallInterceptor {
	return &staticHeadersInterceptor{headers: headers}
}

func (s *staticHeadersInterceptor) Before(ctx context.Context, req *a2aclient.Request) (context.Context, any, error) {
	for k, v := range s.headers {
		if v != "" {
			req.ServiceParams.Append(k, v)
		}
	}
	return ctx, nil, nil
}

// upstreamAuthInterceptor applies per-request auth when the controller proxies an A2A call
// to a managed agent. Auth must be evaluated per request because the session principal is only
// available in the call context, not at agent registration time. It also propagates W3C
// TraceContext so distributed traces span across the controller→agent hop without agents
// needing to handle propagation themselves.
type upstreamAuthInterceptor struct {
	a2aclient.PassthroughInterceptor
	authProvider auth.AuthProvider
	agentRef     types.NamespacedName
}

func NewUpstreamAuthInterceptor(authProvider auth.AuthProvider, agentRef types.NamespacedName) a2aclient.CallInterceptor {
	return &upstreamAuthInterceptor{
		authProvider: authProvider,
		agentRef:     agentRef,
	}
}

func (u *upstreamAuthInterceptor) Before(ctx context.Context, req *a2aclient.Request) (context.Context, any, error) {
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, req.BaseURL, nil)
	if err != nil {
		return ctx, nil, err
	}
	if session, ok := auth.AuthSessionFrom(ctx); ok {
		upstreamPrincipal := auth.Principal{
			Agent: auth.Agent{
				ID: u.agentRef.String(),
			},
		}
		if err := u.authProvider.UpstreamAuth(httpReq, session, upstreamPrincipal); err != nil {
			return ctx, nil, err
		}
	}
	propagation.TraceContext{}.Inject(ctx, propagation.HeaderCarrier(httpReq.Header))
	for k, values := range httpReq.Header {
		for _, value := range values {
			req.ServiceParams.Append(k, value)
		}
	}
	return ctx, nil, nil
}
