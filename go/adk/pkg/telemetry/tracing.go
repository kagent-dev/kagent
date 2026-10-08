package telemetry

import (
	"context"

	kagenttelemetry "github.com/kagent-dev/kagent/go/pkg/telemetry"
	"github.com/kagent-dev/kagent/go/pkg/tracing"
	"go.opentelemetry.io/contrib/processors/baggagecopy"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
)

// Init boots the process telemetry. Baggagecopy runs before the request
// attribute processor so runtime identity still wins when both set a key.
func Init(ctx context.Context, telemetry tracing.RuntimeTelemetry) (*kagenttelemetry.Providers, error) {
	processors := make([]sdktrace.SpanProcessor, 0, 2)
	if filter := AllowedBaggageCopyFilter(); filter != nil {
		processors = append(processors, baggagecopy.NewSpanProcessor(filter))
	}
	processors = append(processors, requestAttributesSpanProcessor{})
	return kagenttelemetry.Init(ctx, kagenttelemetry.Options{
		Runtime:        string(telemetry.Runtime),
		Defaults:       telemetry.ResourceDefaults(""),
		SpanProcessors: processors,
	})
}
