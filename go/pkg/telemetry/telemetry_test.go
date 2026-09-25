package telemetry

import (
	"context"
	"errors"
	"os"
	"slices"
	"strings"
	"testing"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	semconv "go.opentelemetry.io/otel/semconv/v1.43.0"

	"github.com/kagent-dev/kagent/go/pkg/telemetry/conv"
)

func restoreGlobals(t *testing.T) {
	t.Helper()
	tracer, meter, propagator := otel.GetTracerProvider(), otel.GetMeterProvider(), otel.GetTextMapPropagator()
	t.Cleanup(func() {
		otel.SetTracerProvider(tracer)
		otel.SetMeterProvider(meter)
		otel.SetTextMapPropagator(propagator)
	})
}

func setExporters(t *testing.T, traces, metrics, logs string) {
	t.Helper()
	t.Setenv("OTEL_TRACES_EXPORTER", traces)
	t.Setenv("OTEL_METRICS_EXPORTER", metrics)
	t.Setenv("OTEL_LOGS_EXPORTER", logs)
}

func resourceAttributes(t *testing.T, opts Options) map[string]string {
	t.Helper()
	res, err := newResource(t.Context(), opts)
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]string{}
	for _, kv := range res.Attributes() {
		got[string(kv.Key)] = kv.Value.String()
	}
	return got
}

func TestResourceEnvironmentWinsOverDefaults(t *testing.T) {
	t.Setenv("OTEL_SERVICE_NAME", "demo-claude")
	t.Setenv("OTEL_RESOURCE_ATTRIBUTES", "service.namespace=team,gen_ai.agent.name=demo-claude,kagent.runtime=forged")
	got := resourceAttributes(t, Options{
		Runtime:  conv.KagentRuntimeClaude,
		Defaults: []attribute.KeyValue{semconv.ServiceName("fallback"), semconv.ServiceNamespace("default")},
	})
	for key, want := range map[string]string{
		"service.name":           "demo-claude",
		"service.namespace":      "team",
		"gen_ai.agent.name":      "demo-claude",
		"kagent.runtime":         conv.KagentRuntimeClaude,
		"telemetry.sdk.language": "go",
	} {
		if got[key] != want {
			t.Errorf("%s = %q, want %q", key, got[key], want)
		}
	}
}

func TestResourceDefaultsFillUnsetKeys(t *testing.T) {
	t.Setenv("OTEL_SERVICE_NAME", "")
	t.Setenv("OTEL_RESOURCE_ATTRIBUTES", "")
	got := resourceAttributes(t, Options{Defaults: []attribute.KeyValue{semconv.ServiceName("kagent-controller")}})
	if got["service.name"] != "kagent-controller" {
		t.Fatalf("service.name = %q, want the default", got["service.name"])
	}
	if _, ok := got["kagent.runtime"]; ok {
		t.Fatal("kagent.runtime set without a runtime")
	}
}

func TestInitLeavesDisabledSignalsOff(t *testing.T) {
	for name, env := range map[string]map[string]string{
		"exporters none": {"OTEL_TRACES_EXPORTER": "none", "OTEL_METRICS_EXPORTER": "none", "OTEL_LOGS_EXPORTER": "none"},
		"sdk disabled":   {"OTEL_TRACES_EXPORTER": "console", "OTEL_METRICS_EXPORTER": "console", "OTEL_LOGS_EXPORTER": "console", "OTEL_SDK_DISABLED": "true"},
	} {
		t.Run(name, func(t *testing.T) {
			restoreGlobals(t)
			for key, value := range env {
				t.Setenv(key, value)
			}
			providers, err := Init(t.Context(), Options{})
			if err != nil {
				t.Fatal(err)
			}
			if providers.TracesEnabled() || providers.meter != nil || providers.logger != nil {
				t.Fatal("a disabled signal got a provider")
			}
			if err := providers.ForceFlush(t.Context()); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestInitKeepsWorkingSignalsWhenOneFails(t *testing.T) {
	restoreGlobals(t)
	setExporters(t, "not-an-exporter", "none", "console")
	providers, err := Init(t.Context(), Options{})
	if err == nil || !strings.Contains(err.Error(), "traces") {
		t.Fatalf("error = %v, want the traces failure", err)
	}
	if providers.TracesEnabled() || providers.logger == nil {
		t.Fatal("a failing signal must stay off without disabling the others")
	}
	t.Cleanup(func() { _ = providers.Shutdown(context.Background()) })
}

func TestInitKeepsSignalsWithAMalformedResourceAttribute(t *testing.T) {
	restoreGlobals(t)
	setExporters(t, "console", "none", "none")
	t.Setenv("OTEL_RESOURCE_ATTRIBUTES", "team=a,b,service.instance.id=uid")
	providers, err := Init(t.Context(), Options{})
	if err == nil || !strings.Contains(err.Error(), "partial resource") {
		t.Fatalf("error = %v, want the partial resource reported", err)
	}
	t.Cleanup(func() { _ = providers.Shutdown(context.Background()) })
	if !providers.TracesEnabled() {
		t.Fatal("a malformed resource attribute turned traces off")
	}
}

func TestInitAppliesDefaultsWithoutBaggage(t *testing.T) {
	restoreGlobals(t)
	setExporters(t, "none", "none", "none")
	for _, value := range Defaults {
		t.Setenv(value.Name, "")
	}
	t.Setenv("OTEL_EXPORTER_OTLP_COMPRESSION", "none")
	if _, err := Init(t.Context(), Options{}); err != nil {
		t.Fatal(err)
	}
	if fields := otel.GetTextMapPropagator().Fields(); len(fields) != 2 || fields[0] != "traceparent" {
		t.Fatalf("propagator fields = %v, want trace context only", fields)
	}
	if got := os.Getenv("OTEL_EXPORTER_OTLP_COMPRESSION"); got != "none" {
		t.Fatalf("compression = %q, an explicit setting must win", got)
	}
}

type failingExporter struct {
	*tracetest.InMemoryExporter
	err error
}

func (e failingExporter) ExportSpans(context.Context, []sdktrace.ReadOnlySpan) error {
	return e.err
}

func TestForceFlushExportsForACanceledRequest(t *testing.T) {
	exporter := tracetest.NewInMemoryExporter()
	providers := &Providers{tracer: sdktrace.NewTracerProvider(sdktrace.WithBatcher(exporter, sdktrace.WithBatchTimeout(time.Hour)))}
	t.Cleanup(func() { _ = providers.Shutdown(context.Background()) })
	_, span := providers.tracer.Tracer("test").Start(t.Context(), "request")
	span.End()

	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if err := providers.ForceFlush(ctx); err != nil {
		t.Fatal(err)
	}
	if spans := exporter.GetSpans(); len(spans) != 1 {
		t.Fatalf("exported %d spans, want 1", len(spans))
	}
}

func TestForceFlushReturnsExportError(t *testing.T) {
	want := errors.New("collector unavailable")
	exporter := failingExporter{InMemoryExporter: tracetest.NewInMemoryExporter(), err: want}
	providers := &Providers{tracer: sdktrace.NewTracerProvider(sdktrace.WithBatcher(exporter, sdktrace.WithBatchTimeout(time.Hour)))}
	t.Cleanup(func() { _ = providers.Shutdown(context.Background()) })
	_, span := providers.tracer.Tracer("test").Start(t.Context(), "request")
	span.End()
	if err := providers.ForceFlush(t.Context()); !errors.Is(err, want) {
		t.Fatalf("ForceFlush error = %v, want %v", err, want)
	}
}

func TestWithDefaultsKeepsExplicitSettings(t *testing.T) {
	got := WithDefaults([]string{"PATH=/bin", "OTEL_PROPAGATORS=b3", "OTEL_EXPORTER_OTLP_COMPRESSION="})
	want := []string{
		"PATH=/bin", "OTEL_PROPAGATORS=b3", "OTEL_EXPORTER_OTLP_COMPRESSION=",
		"OTEL_EXPORTER_OTLP_COMPRESSION=gzip",
		"OTEL_EXPORTER_OTLP_METRICS_DEFAULT_HISTOGRAM_AGGREGATION=base2_exponential_bucket_histogram",
	}
	if !slices.Equal(got, want) {
		t.Fatalf("WithDefaults = %v, want %v", got, want)
	}
}

type countingMetricExporter struct {
	exports int
}

func (e *countingMetricExporter) Temporality(kind sdkmetric.InstrumentKind) metricdata.Temporality {
	return sdkmetric.DefaultTemporalitySelector(kind)
}

func (e *countingMetricExporter) Aggregation(kind sdkmetric.InstrumentKind) sdkmetric.Aggregation {
	return sdkmetric.DefaultAggregationSelector(kind)
}

func (e *countingMetricExporter) Export(context.Context, *metricdata.ResourceMetrics) error {
	e.exports++
	return nil
}

func (e *countingMetricExporter) ForceFlush(context.Context) error { return nil }

func (e *countingMetricExporter) Shutdown(context.Context) error { return nil }

func TestForceFlushExportsMetricsWithTracesOff(t *testing.T) {
	exporter := &countingMetricExporter{}
	providers := &Providers{meter: sdkmetric.NewMeterProvider(sdkmetric.WithReader(sdkmetric.NewPeriodicReader(exporter, sdkmetric.WithInterval(time.Hour))))}
	t.Cleanup(func() { _ = providers.Shutdown(context.Background()) })
	counter, err := providers.meter.Meter("test").Int64Counter("turns")
	if err != nil {
		t.Fatal(err)
	}
	counter.Add(t.Context(), 1)
	if err := providers.ForceFlush(t.Context()); err != nil {
		t.Fatal(err)
	}
	if exporter.exports != 1 {
		t.Fatalf("metric exports = %d, want 1", exporter.exports)
	}
}
