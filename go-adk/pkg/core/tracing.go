package core

import (
	"context"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
)

// SetKAgentSpanAttributes sets kagent span attributes in the OpenTelemetry context
func SetKAgentSpanAttributes(ctx context.Context, attributes map[string]string) context.Context {
	span := trace.SpanFromContext(ctx)
	if span.IsRecording() {
		for key, value := range attributes {
			if value != "" {
				span.SetAttributes(attribute.String(key, value))
			}
		}
	}
	return ctx
}

// ClearKAgentSpanAttributes clears kagent span attributes (no-op in Go, context is immutable)
func ClearKAgentSpanAttributes(ctx context.Context) context.Context {
	// In Go, we don't need to explicitly clear attributes as context is immutable
	// The span attributes are set on the span itself, not in context
	return ctx
}
