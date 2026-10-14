package telemetry

import (
	"context"

	sdktrace "go.opentelemetry.io/otel/sdk/trace"

	kagenttelemetry "github.com/kagent-dev/kagent/go/pkg/telemetry"
	"github.com/kagent-dev/kagent/go/pkg/tracing"
)

// Init boots the process telemetry with the request attribute span processor.
func Init(ctx context.Context, telemetry tracing.RuntimeTelemetry) (*kagenttelemetry.Providers, error) {
	return kagenttelemetry.Init(ctx, kagenttelemetry.Options{
		Identity:       telemetry.ResourceIdentity(),
		Config:         telemetry.ExporterConfig(),
		Defaults:       telemetry.ResourceDefaults(""),
		SpanProcessors: []sdktrace.SpanProcessor{requestAttributesSpanProcessor{}},
	})
}
