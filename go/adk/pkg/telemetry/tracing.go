package telemetry

import (
	"context"
	"fmt"
	"os"

	"go.opentelemetry.io/contrib/processors/baggagecopy"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"

	"github.com/kagent-dev/kagent/go/core/pkg/env"
	kagenttelemetry "github.com/kagent-dev/kagent/go/pkg/telemetry"
	"github.com/kagent-dev/kagent/go/pkg/tracing"
)

// Init installs caller baggage promotion before the authoritative request attributes.
// Call it before the first ADK invocation, which caches the SDK capture mode.
func Init(ctx context.Context, telemetry tracing.RuntimeTelemetry) (*kagenttelemetry.Providers, error) {
	// ADK only accepts capture configuration through its OTEL variable. Supply
	// the runtime config as a default, preserving explicit SDK overrides.
	if _, set := env.OTELGenAICaptureMessageContent.Lookup(); !set {
		capture := tracing.CaptureContentDisabled
		if telemetry.CaptureContent {
			capture = tracing.CaptureContentSpanOnly
		}
		if err := os.Setenv(env.OTELGenAICaptureMessageContent.Name(), capture); err != nil {
			return nil, fmt.Errorf("failed to configure ADK content capture: %w", err)
		}
	}
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
