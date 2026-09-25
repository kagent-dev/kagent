package telemetry

import (
	"context"

	kagenttelemetry "github.com/kagent-dev/kagent/go/pkg/telemetry"
	"github.com/kagent-dev/kagent/go/pkg/tracing"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/trace"
)

// SetKAgentSpanAttributes sets kagent span attributes in the OpenTelemetry context
func SetKAgentSpanAttributes(ctx context.Context, attributes map[string]string) context.Context {
	merged := mergeAttributes(contextAttributes(ctx), attributes)
	setSpanAttributes(ctx, stringAttributes(merged)...)
	if len(merged) == 0 {
		return ctx
	}
	return context.WithValue(ctx, kagentSpanAttributesKey{}, merged)
}

// StartInvocationSpan creates a lightweight span around one executor run,
// beneath the invocation span the A2A server opened. Descendant spans inherit
// request-scoped attributes via the span processor.
func StartInvocationSpan(ctx context.Context) (context.Context, trace.Span) {
	return tracing.Tracer("gcp.vertex.agent").Start(ctx, "invocation")
}

// Init boots the process telemetry with the kagent span processor.
func Init(ctx context.Context, telemetry tracing.RuntimeTelemetry) (*kagenttelemetry.Providers, error) {
	return kagenttelemetry.Init(ctx, kagenttelemetry.Options{
		Runtime:        string(telemetry.Runtime),
		Defaults:       telemetry.ResourceDefaults(""),
		SpanProcessors: []sdktrace.SpanProcessor{kagentAttributesSpanProcessor{}},
	})
}
