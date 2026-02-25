package telemetry

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
