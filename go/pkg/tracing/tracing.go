// Package tracing holds the invocation span contract shared by kagent runtimes.
package tracing

import (
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"

	"github.com/kagent-dev/kagent/go/pkg/telemetry/conv"
)

// Tracer returns the global tracer for an instrumentation scope, declaring the
// semantic conventions version this contract follows. Every span kagent starts
// goes through it, so no scope is left without a schema URL.
func Tracer(scope string) trace.Tracer {
	return otel.Tracer(scope, trace.WithSchemaURL(SchemaURL))
}

// Meter returns the global meter for an instrumentation scope with the same
// schema URL as Tracer.
func Meter(scope string) metric.Meter {
	return otel.Meter(scope, metric.WithSchemaURL(SchemaURL))
}

// InvocationDuration creates the gen_ai.invoke_agent.duration histogram.
func InvocationDuration(meter metric.Meter) (metric.Float64Histogram, error) {
	return meter.Float64Histogram(conv.GenAIInvokeAgentDuration,
		metric.WithUnit(conv.GenAIInvokeAgentDurationUnit),
		metric.WithDescription(conv.GenAIInvokeAgentDurationDescription))
}
