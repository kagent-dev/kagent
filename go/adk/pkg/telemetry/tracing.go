package telemetry

import (
	"context"

	"go.opentelemetry.io/contrib/processors/baggagecopy"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"

	kagenttelemetry "github.com/kagent-dev/kagent/go/pkg/telemetry"
	"github.com/kagent-dev/kagent/go/pkg/tracing"
)

// Init installs caller baggage promotion before the authoritative request attributes.
func Init(ctx context.Context, telemetry tracing.RuntimeTelemetry) (*kagenttelemetry.Providers, error) {
	processors := make([]sdktrace.SpanProcessor, 0, 2)
	if filter := AllowedBaggageCopyFilter(); filter != nil {
		processors = append(processors, baggagecopy.NewSpanProcessor(filter))
	}
	processors = append(processors, requestAttributesSpanProcessor{})
	return kagenttelemetry.Init(ctx, kagenttelemetry.Options{
		Identity:       telemetry.ResourceIdentity(),
		Config:         telemetry.ExporterConfig(),
		Defaults:       telemetry.ResourceDefaults(""),
		SpanProcessors: processors,
	})
}
