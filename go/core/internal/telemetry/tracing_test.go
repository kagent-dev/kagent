package telemetry

import (
	"context"
	"net/http"
	"testing"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/propagation"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
)

// restoreGlobals puts the process-wide OTEL registrations back after a test.
func restoreGlobals(t *testing.T) {
	t.Helper()
	tracerProvider := otel.GetTracerProvider()
	propagator := otel.GetTextMapPropagator()
	t.Cleanup(func() {
		otel.SetTracerProvider(tracerProvider)
		otel.SetTextMapPropagator(propagator)
	})
}

func TestInitTracerProviderDisabled(t *testing.T) {
	restoreGlobals(t)
	t.Setenv("OTEL_TRACING_ENABLED", "false")

	before := otel.GetTracerProvider()
	shutdown, err := InitTracerProvider(context.Background(), "test")
	if err != nil {
		t.Fatal(err)
	}
	if err := shutdown(context.Background()); err != nil {
		t.Fatalf("shutdown: %v", err)
	}
	if otel.GetTracerProvider() != before {
		t.Fatal("disabled tracing replaced the global TracerProvider")
	}
}

func TestInitTracerProviderRegistersGlobals(t *testing.T) {
	restoreGlobals(t)
	t.Setenv("OTEL_TRACING_ENABLED", "true")
	// "none" selects a noop exporter, so the test dials no collector.
	t.Setenv("OTEL_TRACES_EXPORTER", "none")

	shutdown, err := InitTracerProvider(context.Background(), "test")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = shutdown(context.Background()) })

	if _, ok := otel.GetTracerProvider().(*sdktrace.TracerProvider); !ok {
		t.Fatalf("global TracerProvider = %T, want *sdktrace.TracerProvider", otel.GetTracerProvider())
	}

	ctx, span := otel.Tracer("test").Start(context.Background(), "span")
	defer span.End()
	header := http.Header{}
	otel.GetTextMapPropagator().Inject(ctx, propagation.HeaderCarrier(header))
	if header.Get("traceparent") == "" {
		t.Fatal("registered propagator did not inject traceparent")
	}
}
