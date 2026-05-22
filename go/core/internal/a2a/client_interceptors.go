package a2a

import (
	"context"
	"net/http"

	"github.com/a2aproject/a2a-go/v2/a2aclient"
	"github.com/kagent-dev/kagent/go/core/pkg/auth"
	"go.opentelemetry.io/otel/propagation"
	"k8s.io/apimachinery/pkg/types"
)

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
