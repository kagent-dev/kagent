package telemetry

import (
	"context"

	kagenttelemetry "github.com/kagent-dev/kagent/go/pkg/telemetry"
	"github.com/kagent-dev/kagent/go/pkg/tracing"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
)

// Init boots the process telemetry with the request attribute span processor.
func Init(ctx context.Context, telemetry tracing.RuntimeTelemetry) (*kagenttelemetry.Providers, error) {
	return kagenttelemetry.Init(ctx, kagenttelemetry.Options{
		Runtime:        string(telemetry.Runtime),
		Defaults:       telemetry.ResourceDefaults(""),
		SpanProcessors: []sdktrace.SpanProcessor{requestAttributesSpanProcessor{}},
	})
}
