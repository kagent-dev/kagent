package server

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"slices"
	"strings"
	"testing"

	a2atype "github.com/a2aproject/a2a-go/v2/a2a"
	a2apb "github.com/a2aproject/a2a-go/v2/a2apb/v1"
	"github.com/a2aproject/a2a-go/v2/a2apb/v1/pbconv"
	"github.com/kagent-dev/kagent/go/pkg/telemetry/telemetrytest"
	"github.com/kagent-dev/kagent/go/pkg/tracing"
	"go.opentelemetry.io/otel"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.opentelemetry.io/otel/trace"
	"google.golang.org/grpc/health/grpc_health_v1"
)

// The ADK transport gives every A2A call one SERVER span, from otelgrpc for
// gRPC and from otelhttp for JSON-RPC, and traces no health check.
func TestServerTelemetryShape(t *testing.T) {
	spans := tracetest.NewInMemoryExporter()
	reader := sdkmetric.NewManualReader()
	tracerProvider := sdktrace.NewTracerProvider(sdktrace.WithSyncer(spans))
	meterProvider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	previousTracer, previousMeter := otel.GetTracerProvider(), otel.GetMeterProvider()
	otel.SetTracerProvider(tracerProvider)
	otel.SetMeterProvider(meterProvider)
	t.Cleanup(func() {
		otel.SetTracerProvider(previousTracer)
		otel.SetMeterProvider(previousMeter)
		_ = tracerProvider.Shutdown(context.Background())
		_ = meterProvider.Shutdown(context.Background())
	})
	testServer, conn := startTestServer(t)

	request, err := pbconv.ToProtoSendMessageRequest(&a2atype.SendMessageRequest{
		Message: a2atype.NewMessage(a2atype.MessageRoleUser, a2atype.NewTextPart("hi")),
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := a2apb.NewA2AServiceClient(conn).SendMessage(t.Context(), request); err != nil {
		t.Fatalf("gRPC SendMessage: %v", err)
	}
	if _, err := grpc_health_v1.NewHealthClient(conn).Check(t.Context(), &grpc_health_v1.HealthCheckRequest{}); err != nil {
		t.Fatalf("gRPC health check: %v", err)
	}
	if response, err := testServer.Client().Get(testServer.URL + "/healthz"); err != nil {
		t.Fatalf("GET /healthz: %v", err)
	} else {
		_ = response.Body.Close()
	}
	body, err := json.Marshal(map[string]any{
		"jsonrpc": "2.0", "id": "1", "method": "SendMessage",
		"params": &a2atype.SendMessageRequest{Message: a2atype.NewMessage(a2atype.MessageRoleUser, a2atype.NewTextPart("hi"))},
	})
	if err != nil {
		t.Fatal(err)
	}
	httpRequest, err := http.NewRequestWithContext(t.Context(), http.MethodPost, testServer.URL, bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	httpRequest.Header.Set("Content-Type", "application/json")
	httpRequest.Header.Set(a2atype.SvcParamVersion, string(a2atype.Version))
	response, err := testServer.Client().Do(httpRequest)
	if err != nil {
		t.Fatalf("JSON-RPC SendMessage: %v", err)
	}
	_ = response.Body.Close()

	exported := spans.GetSpans()
	telemetrytest.AssertTraceShape(t, exported)
	var servers []string
	transports := map[trace.SpanID]tracetest.SpanStub{}
	for _, span := range exported {
		if strings.Contains(span.Name, "Health") || strings.Contains(span.Name, "healthz") {
			t.Errorf("health check traced as %q", span.Name)
		}
		if span.SpanKind == trace.SpanKindServer {
			servers = append(servers, span.Name)
		}
		if span.Name == tracing.TransportSpanName {
			transports[span.Parent.SpanID()] = span
		}
	}
	slices.Sort(servers)
	if want := []string{"POST /", "lf.a2a.v1.A2AService/SendMessage"}; !slices.Equal(servers, want) {
		t.Fatalf("SERVER spans = %v, want %v", servers, want)
	}
	for _, span := range exported {
		if span.SpanKind != trace.SpanKindServer {
			continue
		}
		if _, ok := transports[span.SpanContext.SpanID()]; !ok {
			t.Errorf("SERVER span %q has no %s child", span.Name, tracing.TransportSpanName)
		}
	}

	data := telemetrytest.Collect(t, reader)
	for _, want := range []telemetrytest.MetricShape{
		{Name: "rpc.server.call.duration", Kind: "histogram", Unit: "s", AttributeKeys: []string{"rpc.method", "rpc.response.status_code", "rpc.system.name"}},
		{Name: "http.server.request.duration", Kind: "histogram", Unit: "s", AttributeKeys: []string{
			"http.request.method", "http.response.status_code", "http.route",
			"network.protocol.name", "network.protocol.version", "server.address", "server.port", "url.scheme",
		}},
	} {
		got, ok := telemetrytest.FindMetric(data, want.Name)
		if !ok {
			t.Errorf("%s was not recorded", want.Name)
			continue
		}
		if got.Kind != want.Kind || got.Unit != want.Unit || !slices.Equal(got.AttributeKeys, want.AttributeKeys) {
			t.Errorf("%s = %+v, want %+v", want.Name, got, want)
		}
	}
}
