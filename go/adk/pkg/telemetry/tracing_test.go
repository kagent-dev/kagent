package telemetry

import (
	"compress/gzip"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/kagent-dev/kagent/go/pkg/tracing"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/log"
	logglobal "go.opentelemetry.io/otel/log/global"
	lognoop "go.opentelemetry.io/otel/log/noop"
	tracenoop "go.opentelemetry.io/otel/trace/noop"
	collectortrace "go.opentelemetry.io/proto/otlp/collector/trace/v1"
	"google.golang.org/protobuf/proto"
)

func TestInitExportsConfiguredSignals(t *testing.T) {
	for _, tc := range []struct {
		name         string
		traces, logs bool
		serviceName  string
		wantService  string
	}{
		{name: "disabled"},
		{name: "traces", traces: true, wantService: "adk-service"},
		{name: "logs", logs: true},
		{name: "both", traces: true, logs: true, serviceName: "demo-kagent", wantService: "demo-kagent"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var mu sync.Mutex
			received := map[string][]byte{}
			collector := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				body := io.Reader(r.Body)
				if r.Header.Get("Content-Encoding") != "gzip" {
					t.Errorf("export to %s is not gzip-compressed", r.URL.Path)
				} else if reader, err := gzip.NewReader(r.Body); err == nil {
					body = reader
				}
				data, err := io.ReadAll(body)
				if err != nil {
					t.Error(err)
					w.WriteHeader(http.StatusBadRequest)
					return
				}
				mu.Lock()
				received[r.URL.Path] = data
				mu.Unlock()
				w.Header().Set("Content-Type", "application/x-protobuf")
			}))
			defer collector.Close()
			t.Setenv("OTEL_TRACES_EXPORTER", exporter(tc.traces))
			t.Setenv("OTEL_METRICS_EXPORTER", "none")
			t.Setenv("OTEL_LOGS_EXPORTER", exporter(tc.logs))
			t.Setenv("OTEL_SERVICE_NAME", tc.serviceName)
			t.Setenv("OTEL_EXPORTER_OTLP_PROTOCOL", "http/protobuf")
			t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", collector.URL)
			t.Setenv("OTEL_BSP_SCHEDULE_DELAY", "3600000")
			t.Setenv("OTEL_EXPORTER_OTLP_COMPRESSION", "")
			oldTrace, oldLog, oldPropagation := otel.GetTracerProvider(), logglobal.GetLoggerProvider(), otel.GetTextMapPropagator()
			t.Cleanup(func() {
				otel.SetTracerProvider(oldTrace)
				logglobal.SetLoggerProvider(oldLog)
				otel.SetTextMapPropagator(oldPropagation)
			})
			otel.SetTracerProvider(tracenoop.NewTracerProvider())
			logglobal.SetLoggerProvider(lognoop.NewLoggerProvider())
			providers, err := Init(t.Context(), tracing.RuntimeTelemetry{
				Runtime: tracing.RuntimeADKGo, AgentName: "adk-service", AgentNamespace: "agent-namespace",
			})
			if err != nil {
				t.Fatal(err)
			}
			if providers.TracesEnabled() != tc.traces {
				t.Fatalf("traces enabled = %v", providers.TracesEnabled())
			}
			ctx := SetKAgentSpanAttributes(t.Context(), map[string]string{"kagent.test": "inherited"})
			_, span := StartInvocationSpan(ctx)
			span.End()
			var record log.Record
			record.SetBody(attribute.StringValue("test log"))
			logglobal.GetLoggerProvider().Logger("test").Emit(t.Context(), record)
			if err := providers.Shutdown(t.Context()); err != nil {
				t.Fatal(err)
			}
			mu.Lock()
			defer mu.Unlock()
			for path, want := range map[string]bool{"/v1/traces": tc.traces, "/v1/logs": tc.logs} {
				if _, got := received[path]; got != want {
					t.Fatalf("export to %s = %v, want %v", path, got, want)
				}
			}
			if tc.traces {
				var request collectortrace.ExportTraceServiceRequest
				if err := proto.Unmarshal(received["/v1/traces"], &request); err != nil {
					t.Fatal(err)
				}
				if len(request.ResourceSpans) != 1 {
					t.Fatalf("resource spans = %v", request.ResourceSpans)
				}
				resource := request.ResourceSpans[0]
				attrs := map[string]string{}
				for _, attr := range resource.Resource.Attributes {
					attrs[attr.Key] = attr.Value.GetStringValue()
				}
				if attrs["service.name"] != tc.wantService || attrs["service.namespace"] != "agent-namespace" || attrs["kagent.runtime"] != "adk-go" {
					t.Fatalf("resource = %v", attrs)
				}
				found := false
				for _, scope := range resource.ScopeSpans {
					for _, span := range scope.Spans {
						for _, attr := range span.Attributes {
							if span.Name == "invocation" && attr.Key == "kagent.test" && attr.Value.GetStringValue() == "inherited" {
								found = true
							}
						}
					}
				}
				if !found {
					t.Fatal("ADK attribute processor did not enrich the exported invocation span")
				}
			}
		})
	}
}

// A disabled initializer must leave an application's existing provider alone.
func TestInitDisabledPreservesProvider(t *testing.T) {
	t.Setenv("OTEL_TRACES_EXPORTER", "none")
	t.Setenv("OTEL_METRICS_EXPORTER", "none")
	t.Setenv("OTEL_LOGS_EXPORTER", "none")
	previous, previousPropagator := otel.GetTracerProvider(), otel.GetTextMapPropagator()
	t.Cleanup(func() { otel.SetTextMapPropagator(previousPropagator) })
	providers, err := Init(t.Context(), tracing.RuntimeTelemetry{Runtime: tracing.RuntimeADKGo, AgentName: "unused", AgentNamespace: "unused"})
	if err != nil || providers.TracesEnabled() {
		t.Fatalf("Init = traces %v, error %v", providers.TracesEnabled(), err)
	}
	if otel.GetTracerProvider() != previous {
		t.Fatal("disabled Init replaced the provider")
	}
	if err := providers.Shutdown(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func exporter(enabled bool) string {
	if enabled {
		return "otlp"
	}
	return "none"
}
