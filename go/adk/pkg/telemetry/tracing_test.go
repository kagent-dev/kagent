package telemetry

import (
	"context"
	"testing"
	"time"

	"go.opentelemetry.io/otel"
	logapi "go.opentelemetry.io/otel/log"
	logglobal "go.opentelemetry.io/otel/log/global"
	sdklog "go.opentelemetry.io/otel/sdk/log"
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

// TestForceFlushFlushesLogs verifies that ForceFlush drains records queued by
// the batch log processor even when the tracer provider is not flushable.
func TestForceFlushFlushesLogs(t *testing.T) {
	exporter := &testLogExporter{}
	lp := sdklog.NewLoggerProvider(
		sdklog.WithProcessor(sdklog.NewBatchProcessor(exporter)),
	)
	prev := logglobal.GetLoggerProvider()
	logglobal.SetLoggerProvider(lp)
	t.Cleanup(func() {
		logglobal.SetLoggerProvider(prev)
		_ = lp.Shutdown(context.Background())
	})

	logger := logglobal.GetLoggerProvider().Logger("test")
	var record logapi.Record
	record.SetEventName("buffered")
	logger.Emit(context.Background(), record)

	if got := exporter.count(); got != 0 {
		t.Fatalf("log exported before flush: %d", got)
	}

	// A canceled request context must not prevent the flush.
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	ForceFlush(ctx)

	if got := exporter.count(); got != 1 {
		t.Fatalf("expected 1 log after flush, got %d", got)
	}
}

type testLogExporter struct {
	records []sdklog.Record
}

func (e *testLogExporter) Export(_ context.Context, records []sdklog.Record) error {
	e.records = append(e.records, records...)
	return nil
}

func (e *testLogExporter) Shutdown(context.Context) error {
	return nil
}

func (e *testLogExporter) ForceFlush(context.Context) error {
	return nil
}

func (e *testLogExporter) count() int {
	return len(e.records)
}

// flushTimeout reads KAGENT_TELEMETRY_FLUSH_TIMEOUT_MS and falls back to 3s on
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
			t.Setenv("KAGENT_TELEMETRY_FLUSH_TIMEOUT_MS", tt.env)
			if got := flushTimeout(); got != tt.want {
				t.Errorf("flushTimeout() = %v, want %v", got, tt.want)
			}
		})
	}
}

// The resource must merge OTEL_RESOURCE_ATTRIBUTES and the telemetry.sdk.*
// attributes. resource.New starts empty, so building it from WithAttributes
// alone silently drops everything the environment supplies.
func TestNewTelemetryResourceMergesEnvAttributes(t *testing.T) {
	t.Setenv("OTEL_SERVICE_NAME", "should-not-win")
	t.Setenv("OTEL_RESOURCE_ATTRIBUTES", "deployment.environment.name=prod,service.version=1.4.2")

	res, err := newTelemetryResource(context.Background(), "svc", "ns")
	if err != nil {
		t.Fatalf("newTelemetryResource: %v", err)
	}

	got := map[string]string{}
	for _, kv := range res.Attributes() {
		got[string(kv.Key)] = kv.Value.String()
	}
	for key, want := range map[string]string{
		"deployment.environment.name": "prod",
		"service.version":             "1.4.2",
		"telemetry.sdk.language":      "go",
		"service.name":                "svc",
		"service.namespace":           "ns",
	} {
		if got[key] != want {
			t.Errorf("attribute %s = %q, want %q (all: %v)", key, got[key], want, got)
		}
	}
}
