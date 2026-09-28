package telemetry

import (
	"context"

	"go.opentelemetry.io/otel/attribute"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
)

type requestAttributesKey struct{}

// WithRequestAttributes stamps attributes on every span started under ctx, so
// the spans ADK emits carry the request identity it does not know about.
func WithRequestAttributes(ctx context.Context, attributes ...attribute.KeyValue) context.Context {
	if len(attributes) == 0 {
		return ctx
	}
	return context.WithValue(ctx, requestAttributesKey{}, attributes)
}

type requestAttributesSpanProcessor struct{}

func (requestAttributesSpanProcessor) OnStart(parent context.Context, span sdktrace.ReadWriteSpan) {
	if attributes, _ := parent.Value(requestAttributesKey{}).([]attribute.KeyValue); len(attributes) > 0 {
		span.SetAttributes(attributes...)
	}
}

func (requestAttributesSpanProcessor) OnEnd(sdktrace.ReadOnlySpan) {}

func (requestAttributesSpanProcessor) Shutdown(context.Context) error { return nil }

func (requestAttributesSpanProcessor) ForceFlush(context.Context) error { return nil }
