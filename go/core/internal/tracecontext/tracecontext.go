package tracecontext

import (
	"context"
	"net/http"
)

type traceHeadersKeyType struct{}

var traceHeadersKey = traceHeadersKeyType{}

// HeadersFrom retrieves W3C trace context headers (traceparent, tracestate)
// that were captured from an incoming request.
func HeadersFrom(ctx context.Context) (http.Header, bool) {
	v, ok := ctx.Value(traceHeadersKey).(http.Header)
	return v, ok && v != nil
}

// HeadersTo stores W3C trace context headers in the context so they can
// be propagated to outgoing requests (e.g., from the controller to agent pods).
func HeadersTo(ctx context.Context, headers http.Header) context.Context {
	return context.WithValue(ctx, traceHeadersKey, headers)
}
