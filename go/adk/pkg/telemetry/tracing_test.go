package telemetry

import (
	"context"
	"testing"
	"time"

	"go.opentelemetry.io/otel"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.opentelemetry.io/otel/trace/noop"
)

// ForceFlush must drain spans buffered in a batch processor (which would
// otherwise wait out its schedule delay) and be a no-op for providers
// without ForceFlush support (e.g. the default global no-op provider).
func TestForceFlush(t *testing.T) {
	exporter := tracetest.NewInMemoryExporter()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(sdktrace.NewBatchSpanProcessor(exporter)))
	prev := otel.GetTracerProvider()
	otel.SetTracerProvider(tp)
	t.Cleanup(func() {
		otel.SetTracerProvider(prev)
		_ = tp.Shutdown(context.Background())
	})

	_, span := otel.Tracer("test").Start(context.Background(), "buffered")
	span.End()
	if got := len(exporter.GetSpans()); got != 0 {
		t.Fatalf("span exported before flush: %d", got)
	}

	// A canceled request context must not prevent the flush.
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	ForceFlush(ctx)
	if got := len(exporter.GetSpans()); got != 1 {
		t.Fatalf("expected 1 span after flush, got %d", got)
	}

	otel.SetTracerProvider(noop.NewTracerProvider())
	ForceFlush(context.Background()) // must not panic
}

// flushTimeout reads KAGENT_TRACE_FLUSH_TIMEOUT_MS and falls back to 3s on
// unset, non-numeric, or non-positive values.
func TestFlushTimeout(t *testing.T) {
	tests := []struct {
		name string
		env  string
		want time.Duration
	}{
		{name: "unset", env: "", want: 3 * time.Second},
		{name: "valid", env: "500", want: 500 * time.Millisecond},
		{name: "invalid", env: "not-a-number", want: 3 * time.Second},
		{name: "non-positive", env: "0", want: 3 * time.Second},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("KAGENT_TRACE_FLUSH_TIMEOUT_MS", tt.env)
			if got := flushTimeout(); got != tt.want {
				t.Errorf("flushTimeout() = %v, want %v", got, tt.want)
			}
		})
	}
}
