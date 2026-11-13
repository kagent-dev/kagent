package telemetry

import (
	"compress/gzip"
	"context"
	"io"
	"iter"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/log"
	lognoop "go.opentelemetry.io/otel/log/noop"
	tracenoop "go.opentelemetry.io/otel/trace/noop"
	collectorlog "go.opentelemetry.io/proto/otlp/collector/logs/v1"
	collectortrace "go.opentelemetry.io/proto/otlp/collector/trace/v1"
	tracepb "go.opentelemetry.io/proto/otlp/trace/v1"
	"google.golang.org/adk/v2/agent"
	"google.golang.org/adk/v2/agent/llmagent"
	"google.golang.org/adk/v2/model"
	"google.golang.org/adk/v2/runner"
	"google.golang.org/genai"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"

	"github.com/kagent-dev/kagent/go/core/pkg/env"
	"github.com/kagent-dev/kagent/go/pkg/tracing"
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
			t.Setenv("OTEL_EXPORTER_OTLP_COMPRESSION", "gzip")
			oldTrace, oldLog, oldPropagation := otel.GetTracerProvider(), otel.GetLoggerProvider(), otel.GetTextMapPropagator()
			t.Cleanup(func() {
				otel.SetTracerProvider(oldTrace)
				otel.SetLoggerProvider(oldLog)
				otel.SetTextMapPropagator(oldPropagation)
			})
			otel.SetTracerProvider(tracenoop.NewTracerProvider())
			otel.SetLoggerProvider(lognoop.NewLoggerProvider())
			providers, err := Init(t.Context(), tracing.RuntimeTelemetry{
				Runtime: tracing.RuntimeADKGo, AgentName: "adk-service", AgentNamespace: "agent-namespace",
			})
			if err != nil {
				t.Fatal(err)
			}
			if providers.TracesEnabled() != tc.traces {
				t.Fatalf("traces enabled = %v", providers.TracesEnabled())
			}
			ctx := WithRequestAttributes(t.Context(), attribute.String("kagent.test", "inherited"))
			_, span := otel.Tracer("test").Start(ctx, "turn")
			span.End()
			var record log.Record
			record.SetBody(attribute.StringValue("test log"))
			otel.GetLoggerProvider().Logger("test").Emit(t.Context(), record)
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
				if attrs[tracing.AttributeMainAgentName] != "adk-service" || attrs[tracing.AttributeMainAgentID] != "agent-namespace/adk-service" {
					t.Fatalf("resource is missing compiled agent identity: %v", attrs)
				}
				found := false
				for _, scope := range resource.ScopeSpans {
					for _, span := range scope.Spans {
						for _, attr := range span.Attributes {
							if span.Name == "turn" && attr.Key == "kagent.test" && attr.Value.GetStringValue() == "inherited" {
								found = true
							}
						}
					}
				}
				if !found {
					t.Fatal("request attribute processor did not enrich the exported span")
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

func TestADKContentCapture(t *testing.T) {
	scenarios := []struct {
		name      string
		capture   bool
		override  *string
		wantSpans bool
		wantLogs  bool
	}{
		{name: "capture disabled"},
		{name: "capture enabled", capture: true, wantSpans: true},
		{name: "NO_CONTENT override", capture: true, override: new("NO_CONTENT")},
		{name: "false override", capture: true, override: new("false")},
		{name: "SPAN_ONLY override", override: new("SPAN_ONLY"), wantSpans: true},
		{name: "SPAN_AND_EVENT override", override: new("SPAN_AND_EVENT"), wantSpans: true, wantLogs: true},
		{name: "EVENT_ONLY override", capture: true, override: new("EVENT_ONLY"), wantLogs: true},
		{name: "true override", capture: true, override: new("true"), wantLogs: true},
		{name: "empty override", capture: true, override: new("")},
		{name: "invalid override", capture: true, override: new("invalid")},
	}
	for _, scenario := range scenarios {
		t.Run("Given "+scenario.name+", when ADK invokes a model, assert that only configured signals contain content", func(t *testing.T) {
			// ADK caches capture mode on its first call. Each scenario needs a
			// fresh process, just like a newly started managed runtime.
			if os.Getenv("KAGENT_TEST_CONTENT_CAPTURE_CHILD") != "1" {
				binary, err := os.Executable()
				require.NoError(t, err)
				command := exec.CommandContext(t.Context(), binary, "-test.run=^"+strings.ReplaceAll(regexp.QuoteMeta(t.Name()), "/", "$/^")+"$")
				command.Env = append(os.Environ(), "KAGENT_TEST_CONTENT_CAPTURE_CHILD=1")
				output, err := command.CombinedOutput()
				require.NoError(t, err, "%s", output)
				return
			}

			captureVariable := env.OTELGenAICaptureMessageContent.Name()
			require.NoError(t, os.Unsetenv(captureVariable))
			if scenario.override != nil {
				t.Setenv(captureVariable, *scenario.override)
			}
			var mu sync.Mutex
			var spans []*tracepb.Span
			var logCount int
			var logPayloads string
			collector := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				data, err := io.ReadAll(r.Body)
				if err != nil {
					t.Error(err)
					w.WriteHeader(http.StatusBadRequest)
					return
				}
				mu.Lock()
				defer mu.Unlock()
				switch r.URL.Path {
				case "/v1/traces":
					var request collectortrace.ExportTraceServiceRequest
					if err := proto.Unmarshal(data, &request); err != nil {
						t.Error(err)
					}
					for _, resource := range request.ResourceSpans {
						for _, scope := range resource.ScopeSpans {
							spans = append(spans, scope.Spans...)
						}
					}
				case "/v1/logs":
					var request collectorlog.ExportLogsServiceRequest
					if err := proto.Unmarshal(data, &request); err != nil {
						t.Error(err)
					}
					for _, resource := range request.ResourceLogs {
						for _, scope := range resource.ScopeLogs {
							logCount += len(scope.LogRecords)
						}
					}
					encoded, err := protojson.Marshal(&request)
					if err != nil {
						t.Error(err)
					}
					logPayloads += string(encoded)
				default:
					t.Errorf("unexpected OTLP path %s", r.URL.Path)
				}
				w.Header().Set("Content-Type", "application/x-protobuf")
			}))
			t.Cleanup(collector.Close)
			t.Setenv("OTEL_SDK_DISABLED", "false")
			t.Setenv("OTEL_TRACES_EXPORTER", "otlp")
			t.Setenv("OTEL_LOGS_EXPORTER", "otlp")
			t.Setenv("OTEL_METRICS_EXPORTER", "none")
			t.Setenv("OTEL_TRACES_SAMPLER", "always_on")
			for _, signal := range []string{"TRACES", "LOGS"} {
				t.Setenv("OTEL_EXPORTER_OTLP_"+signal+"_ENDPOINT", collector.URL+"/v1/"+strings.ToLower(signal))
				t.Setenv("OTEL_EXPORTER_OTLP_"+signal+"_PROTOCOL", "http/protobuf")
				t.Setenv("OTEL_EXPORTER_OTLP_"+signal+"_COMPRESSION", "none")
			}
			providers, err := Init(t.Context(), tracing.RuntimeTelemetry{CaptureContent: scenario.capture})
			require.NoError(t, err)
			t.Cleanup(func() { require.NoError(t, providers.Shutdown(context.Background())) })
			root, err := llmagent.New(llmagent.Config{
				Name: "capture_test", Model: contentModel{}, Instruction: "Answer concisely.",
			})
			require.NoError(t, err)
			run, err := runner.NewInMemory("capture_test", root)
			require.NoError(t, err)
			for _, err := range run.Run(t.Context(), "user", "session", genai.NewContentFromText("What is 2+2?", genai.RoleUser), agent.RunConfig{}) {
				require.NoError(t, err)
			}
			require.NoError(t, providers.ForceFlush(t.Context()))

			mu.Lock()
			defer mu.Unlock()
			var modelSpans []*tracepb.Span
			for _, span := range spans {
				if strings.HasPrefix(span.Name, "generate_content") {
					modelSpans = append(modelSpans, span)
				}
			}
			require.Len(t, modelSpans, 1, "the model span must exist even when content capture is off")
			attributes := map[string]string{}
			for _, attr := range modelSpans[0].Attributes {
				// ADK records message content as structured values, not JSON strings.
				encoded, err := protojson.Marshal(attr.Value)
				require.NoError(t, err)
				attributes[attr.Key] = string(encoded)
			}
			for key, content := range map[string]string{
				"gen_ai.input.messages": "What is 2+2?", "gen_ai.output.messages": "The answer is 4.",
				"gen_ai.system_instructions": "Answer concisely.",
			} {
				if scenario.wantSpans {
					require.Contains(t, attributes[key], content, key)
				} else {
					require.NotContains(t, attributes, key)
				}
				require.Equal(t, scenario.wantLogs, strings.Contains(logPayloads, content), "log capture of %s", key)
			}
			require.Equal(t, 1, logCount, "ADK emits one inference operation details event even when content capture is off")
		})
	}
}

type contentModel struct{}

var _ model.LLM = contentModel{}

func (contentModel) Name() string { return "capture-model" }

func (contentModel) GenerateContent(context.Context, *model.LLMRequest, bool) iter.Seq2[*model.LLMResponse, error] {
	return func(yield func(*model.LLMResponse, error) bool) {
		yield(&model.LLMResponse{
			Content: genai.NewContentFromText("The answer is 4.", genai.RoleModel), FinishReason: genai.FinishReasonStop,
		}, nil)
	}
}
