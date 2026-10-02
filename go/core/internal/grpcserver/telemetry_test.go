package grpcserver

import (
	"context"
	"net"
	"slices"
	"strings"
	"testing"

	apiv1alpha1 "github.com/kagent-dev/kagent/go/api/gen/kagent/api/v1alpha1"
	"github.com/kagent-dev/kagent/go/pkg/telemetry/telemetrytest"
	"github.com/prometheus/client_golang/prometheus"
	"go.opentelemetry.io/otel"
	otelprometheus "go.opentelemetry.io/otel/exporters/prometheus"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.opentelemetry.io/otel/trace"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/health/grpc_health_v1"
	"google.golang.org/grpc/test/bufconn"
)

// otelgrpc reads the global providers when the server is built, so they are
// installed before New and restored afterwards.
func installServerTelemetry(t *testing.T) (*tracetest.InMemoryExporter, *sdkmetric.ManualReader, *prometheus.Registry) {
	t.Helper()
	spans := tracetest.NewInMemoryExporter()
	reader := sdkmetric.NewManualReader()
	registry := prometheus.NewRegistry()
	scrape, err := otelprometheus.New(otelprometheus.WithRegisterer(registry))
	if err != nil {
		t.Fatal(err)
	}
	tracerProvider := sdktrace.NewTracerProvider(sdktrace.WithSyncer(spans))
	meterProvider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader), sdkmetric.WithReader(scrape))
	previousTracer, previousMeter := otel.GetTracerProvider(), otel.GetMeterProvider()
	otel.SetTracerProvider(tracerProvider)
	otel.SetMeterProvider(meterProvider)
	t.Cleanup(func() {
		otel.SetTracerProvider(previousTracer)
		otel.SetMeterProvider(previousMeter)
		_ = tracerProvider.Shutdown(context.Background())
		_ = meterProvider.Shutdown(context.Background())
	})
	return spans, reader, registry
}

func TestServerTelemetryShape(t *testing.T) {
	spans, reader, registry := installServerTelemetry(t)
	listener := bufconn.Listen(1024 * 1024)
	server, err := New(Config{Listener: listener, SystemService: testSystemService()})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan error, 1)
	go func() { done <- server.Start(ctx) }()
	t.Cleanup(func() {
		cancel()
		<-done
	})
	connection, err := grpc.NewClient("passthrough:///bufnet",
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithContextDialer(func(context.Context, string) (net.Conn, error) { return listener.Dial() }),
	)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = connection.Close() })

	if _, err := apiv1alpha1.NewSystemServiceClient(connection).GetVersion(t.Context(), &apiv1alpha1.GetVersionRequest{}); err != nil {
		t.Fatal(err)
	}
	if _, err := grpc_health_v1.NewHealthClient(connection).Check(t.Context(), &grpc_health_v1.HealthCheckRequest{}); err != nil {
		t.Fatal(err)
	}

	exported := spans.GetSpans()
	telemetrytest.AssertTraceShape(t, exported)
	if len(exported) != 1 {
		t.Fatalf("exported %d spans, want only the GetVersion SERVER span: %v", len(exported), exported)
	}
	if span := exported[0]; span.SpanKind != trace.SpanKindServer || !strings.HasSuffix(span.Name, "SystemService/GetVersion") {
		t.Fatalf("span = %s %q, want the GetVersion SERVER span", span.SpanKind, span.Name)
	}

	shape, ok := telemetrytest.FindMetric(telemetrytest.Collect(t, reader), "rpc.server.call.duration")
	if !ok {
		t.Fatal("rpc.server.call.duration was not recorded")
	}
	want := telemetrytest.MetricShape{
		Name: "rpc.server.call.duration", Kind: "histogram", Unit: "s",
		AttributeKeys: []string{"rpc.method", "rpc.response.status_code", "rpc.system.name"},
	}
	if shape.Name != want.Name || shape.Kind != want.Kind || shape.Unit != want.Unit || !slices.Equal(shape.AttributeKeys, want.AttributeKeys) {
		t.Fatalf("rpc.server.call.duration = %+v, want %+v", shape, want)
	}

	families, err := registry.Gather()
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, family := range families {
		names = append(names, family.GetName())
	}
	if !slices.Contains(names, "rpc_server_call_duration_seconds") {
		t.Fatalf("Prometheus families = %v, want rpc_server_call_duration_seconds", names)
	}
	for _, name := range names {
		if strings.HasPrefix(name, "kagent_grpc_server_") {
			t.Fatalf("Prometheus still serves the removed %s", name)
		}
	}
}
