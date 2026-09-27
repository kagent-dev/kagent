package a2a

import (
	"context"

	"github.com/a2aproject/a2a-go/v2/a2aclient"
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
