package telemetry

import (
	"context"
	"fmt"

	"go.opentelemetry.io/otel/attribute"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/trace"
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

// SetMessageMetadataAttributes sets scalar values from an A2A message's
// metadata as span attributes on the current span.
//
// Keys named in KAGENT_TRACE_CONTEXT_KEYS are skipped. Those values are
// promoted into baggage and copied onto spans by baggagecopy; stamping
// them here would also emit a2a.message.metadata.<from>.
func SetMessageMetadataAttributes(ctx context.Context, metadata map[string]any) {
	if len(metadata) == 0 {
		return
	}
	covered := policyCoveredContextSources()
	var attrs []attribute.KeyValue
	for k, v := range metadata {
		if _, skip := covered[k]; skip {
			continue
		}
		key := "a2a.message.metadata." + k
		switch val := v.(type) {
		case string:
			if val != "" {
				attrs = append(attrs, attribute.String(key, val))
			}
		case bool:
			attrs = append(attrs, attribute.Bool(key, val))
		case float64:
			attrs = append(attrs, attribute.String(key, fmt.Sprintf("%g", val)))
		case int:
			attrs = append(attrs, attribute.Int(key, val))
		case int64:
			attrs = append(attrs, attribute.Int64(key, val))
		}
	}
	setSpanAttributes(ctx, attrs...)
}

func setSpanAttributes(ctx context.Context, attrs ...attribute.KeyValue) {
	span := trace.SpanFromContext(ctx)
	if !span.IsRecording() || len(attrs) == 0 {
		return
	}
	span.SetAttributes(attrs...)
}
