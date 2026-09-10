package tracing

import (
	"context"
	"errors"
	"testing"
	"time"

	"go.opentelemetry.io/otel"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.opentelemetry.io/otel/trace/noop"
)

func TestForceFlushExportsEndedSpans(t *testing.T) {
	exporter := tracetest.NewInMemoryExporter()
	provider := sdktrace.NewTracerProvider(sdktrace.WithBatcher(exporter, sdktrace.WithBatchTimeout(time.Hour)))
	previous := otel.GetTracerProvider()
	otel.SetTracerProvider(provider)
	t.Cleanup(func() {
		otel.SetTracerProvider(previous)
		_ = provider.Shutdown(context.Background())
	})

	_, span := provider.Tracer("test").Start(t.Context(), "request")
	span.End()

	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if err := ForceFlush(ctx); err != nil {
		t.Fatal(err)
	}
	spans := exporter.GetSpans()
	if len(spans) != 1 || spans[0].Name != "request" {
		t.Fatalf("spans exported after ForceFlush = %v, want request span", spans)
	}
}

type failingExporter struct {
	*tracetest.InMemoryExporter
	err error
}

func (e failingExporter) ExportSpans(context.Context, []sdktrace.ReadOnlySpan) error {
	return e.err
}

func TestForceFlushReturnsExportError(t *testing.T) {
	want := errors.New("collector unavailable")
	exporter := failingExporter{InMemoryExporter: tracetest.NewInMemoryExporter(), err: want}
	provider := sdktrace.NewTracerProvider(sdktrace.WithBatcher(exporter, sdktrace.WithBatchTimeout(time.Hour)))
	previous := otel.GetTracerProvider()
	otel.SetTracerProvider(provider)
	t.Cleanup(func() {
		otel.SetTracerProvider(previous)
		_ = provider.Shutdown(context.Background())
	})
	_, span := provider.Tracer("test").Start(t.Context(), "request")
	span.End()
	if err := ForceFlush(t.Context()); !errors.Is(err, want) {
		t.Fatalf("ForceFlush error = %v, want %v", err, want)
	}
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

func TestForceFlushWithoutSDKProvider(t *testing.T) {
	previous := otel.GetTracerProvider()
	otel.SetTracerProvider(noop.NewTracerProvider())
	t.Cleanup(func() { otel.SetTracerProvider(previous) })
	if err := ForceFlush(t.Context()); err != nil {
		t.Fatalf("ForceFlush with no-op provider: %v", err)
	}
}

// The resource must merge OTEL_RESOURCE_ATTRIBUTES and the telemetry.sdk.*
// attributes. resource.New starts empty, so building it from WithAttributes
// alone silently drops everything the environment supplies.
func TestNewTelemetryResourceMergesEnvAttributes(t *testing.T) {
	t.Setenv("OTEL_SERVICE_NAME", "should-not-win")
	t.Setenv("OTEL_RESOURCE_ATTRIBUTES", "deployment.environment.name=prod,service.version=1.4.2")

	res, err := NewResource(context.Background(), "svc", "ns")
	if err != nil {
		t.Fatalf("NewResource: %v", err)
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

func TestOTLPConfigurationPrecedence(t *testing.T) {
	for _, tc := range []struct{ name, generalEndpoint, legacyEndpoint, endpoint, generalProtocol, protocol, wantEndpoint, wantProtocol string }{
		{name: "defaults", wantProtocol: "grpc"},
		{name: "general", generalEndpoint: "http://general:4318", generalProtocol: " HTTP/PROTOBUF ", wantEndpoint: "http://general:4318", wantProtocol: "http/protobuf"},
		{name: "legacy", generalEndpoint: "http://general:4317", legacyEndpoint: "http://legacy:4317", wantEndpoint: "http://legacy:4317", wantProtocol: "grpc"},
		{name: "signal", generalEndpoint: "http://general:4317", legacyEndpoint: "http://legacy:4317", endpoint: " http://signal:4318/v1/traces ", generalProtocol: "grpc", protocol: " HTTP/PROTOBUF ", wantEndpoint: "http://signal:4318/v1/traces", wantProtocol: "http/protobuf"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", tc.generalEndpoint)
			t.Setenv("OTEL_TRACES_EXPORTER_OTLP_ENDPOINT", tc.legacyEndpoint)
			t.Setenv("OTEL_EXPORTER_OTLP_TRACES_ENDPOINT", tc.endpoint)
			t.Setenv("OTEL_EXPORTER_OTLP_PROTOCOL", tc.generalProtocol)
			t.Setenv("OTEL_EXPORTER_OTLP_TRACES_PROTOCOL", tc.protocol)
			if got := OTLPEndpoint("TRACES"); got != tc.wantEndpoint {
				t.Fatalf("endpoint = %q, want %q", got, tc.wantEndpoint)
			}
			if got := OTLPProtocol("TRACES"); got != tc.wantProtocol {
				t.Fatalf("protocol = %q, want %q", got, tc.wantProtocol)
			}
		})
	}
}
