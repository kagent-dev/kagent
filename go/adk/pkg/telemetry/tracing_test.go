package telemetry

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"sync"
	"testing"

	"go.opentelemetry.io/otel"
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
	}{
		{"disabled", false, false}, {"traces", true, false}, {"logs", false, true}, {"both", true, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var mu sync.Mutex
			received := map[string][]byte{}
			collector := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				data, err := io.ReadAll(r.Body)
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
			t.Setenv("OTEL_TRACING_ENABLED", strconv.FormatBool(tc.traces))
			t.Setenv("OTEL_LOGGING_ENABLED", strconv.FormatBool(tc.logs))
			t.Setenv("OTEL_EXPORTER_OTLP_PROTOCOL", "http/protobuf")
			t.Setenv("OTEL_EXPORTER_OTLP_TRACES_PROTOCOL", "")
			t.Setenv("OTEL_EXPORTER_OTLP_LOGS_PROTOCOL", "")
			t.Setenv("OTEL_EXPORTER_OTLP_TRACES_ENDPOINT", collector.URL+"/v1/traces")
			t.Setenv("OTEL_EXPORTER_OTLP_LOGS_ENDPOINT", collector.URL+"/v1/logs")
			t.Setenv("OTEL_BSP_SCHEDULE_DELAY", "3600000")
			oldTrace, oldLog, oldPropagation := otel.GetTracerProvider(), logglobal.GetLoggerProvider(), otel.GetTextMapPropagator()
			t.Cleanup(func() {
				otel.SetTracerProvider(oldTrace)
				logglobal.SetLoggerProvider(oldLog)
				otel.SetTextMapPropagator(oldPropagation)
			})
			otel.SetTracerProvider(tracenoop.NewTracerProvider())
			logglobal.SetLoggerProvider(lognoop.NewLoggerProvider())
			shutdown, enabled, err := Init(t.Context(), "adk-service", "agent-namespace")
			if err != nil {
				t.Fatal(err)
			}
			if enabled != (tc.traces || tc.logs) {
				t.Fatalf("enabled = %v", enabled)
			}
			ctx := SetKAgentSpanAttributes(t.Context(), map[string]string{"kagent.test": "inherited"})
			_, span := StartInvocationSpan(ctx)
			span.End()
			var record log.Record
			record.SetBody(log.StringValue("test log"))
			logglobal.GetLoggerProvider().Logger("test").Emit(t.Context(), record)
			if err := shutdown(t.Context()); err != nil {
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
				if attrs["service.name"] != "adk-service" || attrs["service.namespace"] != "agent-namespace" {
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
	t.Setenv("OTEL_TRACING_ENABLED", "false")
	t.Setenv("OTEL_LOGGING_ENABLED", "false")
	previous := otel.GetTracerProvider()
	shutdown, enabled, err := Init(t.Context(), "unused", "unused")
	if err != nil || enabled {
		t.Fatalf("Init = enabled %v, error %v", enabled, err)
	}
	if otel.GetTracerProvider() != previous {
		t.Fatal("disabled Init replaced the provider")
	}
	if err := shutdown(context.Background()); err != nil {
		t.Fatal(err)
	}
}
