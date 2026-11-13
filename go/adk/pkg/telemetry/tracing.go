package telemetry

import (
	"context"
	"fmt"
	"os"

	sdktrace "go.opentelemetry.io/otel/sdk/trace"

	"github.com/kagent-dev/kagent/go/core/pkg/env"
	kagenttelemetry "github.com/kagent-dev/kagent/go/pkg/telemetry"
	"github.com/kagent-dev/kagent/go/pkg/tracing"
)

// Init boots the process telemetry with the request attribute span processor.
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
	return kagenttelemetry.Init(ctx, kagenttelemetry.Options{
		Identity:       telemetry.ResourceIdentity(),
		Config:         telemetry.ExporterConfig(),
		Defaults:       telemetry.ResourceDefaults(""),
		SpanProcessors: []sdktrace.SpanProcessor{requestAttributesSpanProcessor{}},
	})
}
