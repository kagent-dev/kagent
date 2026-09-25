// Package tracing holds the invocation span contract shared by kagent runtimes.
package tracing

import (
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/trace"
)

// Tracer returns the global tracer for an instrumentation scope, declaring the
// semantic conventions version this contract follows. Every span kagent starts
// goes through it, so no scope is left without a schema URL.
func Tracer(scope string) trace.Tracer {
	return otel.Tracer(scope, trace.WithSchemaURL(SchemaURL))
}
