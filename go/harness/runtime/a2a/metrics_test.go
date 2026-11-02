package a2a

import (
	"context"
	"maps"
	"slices"
	"strings"
	"testing"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"

	"github.com/kagent-dev/kagent/go/harness/runtime"
	"github.com/kagent-dev/kagent/go/pkg/tracing"
)

func TestModelCallRecordsInferenceMetrics(t *testing.T) {
	start := time.Unix(100, 0)
	for _, tc := range []struct {
		name         string
		call         runtime.ModelCall
		wantDuration float64 // zero means no duration point
		wantError    string
		wantUsage    map[string]int64
		wantPerCall  map[string]int64 // the one recorded value of each per-call token histogram
	}{
		{
			name:         "timed call records duration and usage",
			call:         runtime.ModelCall{InputTokens: 10, OutputTokens: 4, CacheReadTokens: 2, Start: start, End: start.Add(2 * time.Second)},
			wantDuration: 2,
			wantUsage: map[string]int64{
				"gen_ai.client.inference.usage.input_tokens":            10,
				"gen_ai.client.inference.usage.output_tokens":           4,
				"gen_ai.client.inference.usage.cache_read.input_tokens": 2,
			},
			wantPerCall: map[string]int64{"gen_ai.client.inference.operation.input_tokens": 10, "gen_ai.client.inference.operation.output_tokens": 4},
		},
		{
			name: "failed call records its error type",
			call: runtime.ModelCall{
				StopReason: tracing.FinishReasonError, ErrorType: "rate_limit", UsagePartial: true,
				Start: start, End: start.Add(time.Second),
			},
			wantDuration: 1,
			wantError:    "rate_limit",
			wantUsage:    map[string]int64{},
			wantPerCall:  map[string]int64{},
		},
		{
			name: "canceled call has no error type",
			call: runtime.ModelCall{
				StopReason: tracing.FinishReasonError, Canceled: true, UsagePartial: true,
				Start: start, End: start.Add(time.Second),
			},
			wantDuration: 1,
			wantUsage:    map[string]int64{},
			wantPerCall:  map[string]int64{},
		},
		{
			name:         "partial call records only its exact input per call",
			call:         runtime.ModelCall{InputTokens: 5, OutputTokens: 2, UsagePartial: true, Start: start, End: start.Add(time.Second)},
			wantDuration: 1,
			wantUsage: map[string]int64{
				"gen_ai.client.inference.usage.input_tokens":  5,
				"gen_ai.client.inference.usage.output_tokens": 2,
			},
			wantPerCall: map[string]int64{"gen_ai.client.inference.operation.input_tokens": 5},
		},
		{
			name: "zero-length call records only usage",
			call: runtime.ModelCall{InputTokens: 3, OutputTokens: 1, ReasoningOutputTokens: 1, Start: start, End: start},
			wantUsage: map[string]int64{
				"gen_ai.client.inference.usage.input_tokens":            3,
				"gen_ai.client.inference.usage.output_tokens":           1,
				"gen_ai.client.inference.usage.reasoning.output_tokens": 1,
			},
			wantPerCall: map[string]int64{"gen_ai.client.inference.operation.input_tokens": 3, "gen_ai.client.inference.operation.output_tokens": 1},
		},
		{
			name: "usage-only tokens add only to the token counters",
			call: runtime.ModelCall{InputTokens: 7, OutputTokens: 90, UsageOnly: true},
			wantUsage: map[string]int64{
				"gen_ai.client.inference.usage.input_tokens":  7,
				"gen_ai.client.inference.usage.output_tokens": 90,
			},
			wantPerCall: map[string]int64{},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			reader := installMetricReader(t)
			tc.call.ResponseModel = "opus-1"
			run := func(_ context.Context, _ runtime.Turn, sink runtime.EventSink) (runtime.Outcome, error) {
				sink.ModelCall(tc.call)
				return runtime.Outcome{}, nil
			}
			executor := newExecutor(t, run, tracing.RuntimeTelemetry{Provider: "anthropic", Model: "opus"})
			for range executor.Execute(t.Context(), requestContext("task-metrics", "hello")) {
			}

			var collected metricdata.ResourceMetrics
			if err := reader.Collect(t.Context(), &collected); err != nil {
				t.Fatal(err)
			}
			var durations []metricdata.HistogramDataPoint[float64]
			usage, perCall := map[string]int64{}, map[string]int64{}
			for _, scope := range collected.ScopeMetrics {
				for _, m := range scope.Metrics {
					if !strings.HasPrefix(m.Name, "gen_ai.client.inference.") {
						// This runtime has no agent name, so no metric carries an empty one.
						var sets []attribute.Set
						switch data := m.Data.(type) {
						case metricdata.Histogram[int64]:
							for _, point := range data.DataPoints {
								sets = append(sets, point.Attributes)
							}
						case metricdata.Histogram[float64]:
							for _, point := range data.DataPoints {
								sets = append(sets, point.Attributes)
							}
						}
						for _, set := range sets {
							if set.HasValue(attribute.Key(tracing.AttributeAgentName)) {
								t.Errorf("%s carries gen_ai.agent.name without an agent name", m.Name)
							}
						}
						continue
					}
					switch data := m.Data.(type) {
					case metricdata.Histogram[float64]:
						durations = append(durations, data.DataPoints...)
					case metricdata.Histogram[int64]:
						for _, point := range data.DataPoints {
							if point.Count != 1 || !slices.Equal(point.Bounds, tokenBuckets) {
								t.Errorf("%s point = %+v, want one value in the advised buckets", m.Name, point)
							}
							perCall[m.Name] = point.Sum
							wantMetricAttributes(t, point.Attributes)
							// The conventions keep per-call token histograms whole, not split by modality.
							if point.Attributes.HasValue("gen_ai.token.modality") {
								t.Errorf("%s carries gen_ai.token.modality", m.Name)
							}
						}
					case metricdata.Sum[int64]:
						for _, point := range data.DataPoints {
							usage[m.Name] += point.Value
							wantMetricAttributes(t, point.Attributes, "gen_ai.token.modality", "unknown")
						}
					}
				}
			}
			if !maps.Equal(usage, tc.wantUsage) {
				t.Errorf("usage = %v, want %v", usage, tc.wantUsage)
			}
			if !maps.Equal(perCall, tc.wantPerCall) {
				t.Errorf("per-call tokens = %v, want %v", perCall, tc.wantPerCall)
			}
			if tc.wantDuration == 0 {
				if len(durations) != 0 {
					t.Fatalf("recorded %d durations, want none", len(durations))
				}
				return
			}
			if len(durations) != 1 || durations[0].Sum != tc.wantDuration || !slices.Equal(durations[0].Bounds, durationBuckets) {
				t.Fatalf("durations = %+v, want one of %vs", durations, tc.wantDuration)
			}
			got, _ := durations[0].Attributes.Value(attribute.Key(tracing.AttributeErrorType))
			if got.AsString() != tc.wantError {
				t.Errorf("error.type = %q, want %q", got.AsString(), tc.wantError)
			}
			wantMetricAttributes(t, durations[0].Attributes)
		})
	}
}

// wantMetricAttributes checks the shared metric attributes, plus extra key-value pairs.
func wantMetricAttributes(t *testing.T, set attribute.Set, extra ...string) {
	t.Helper()
	want := append([]string{
		"gen_ai.operation.name", "chat", "gen_ai.provider.name", "anthropic",
		"gen_ai.request.model", "opus", "gen_ai.response.model", "opus-1",
	}, extra...)
	for i := 0; i < len(want); i += 2 {
		if got, _ := set.Value(attribute.Key(want[i])); got.AsString() != want[i+1] {
			t.Errorf("%s = %q, want %q", want[i], got.AsString(), want[i+1])
		}
	}
	if set.HasValue("gen_ai.conversation.id") {
		t.Error("metric carries gen_ai.conversation.id")
	}
}

func TestToolAndInvocationMetrics(t *testing.T) {
	reader := installMetricReader(t)
	run := func(_ context.Context, _ runtime.Turn, sink runtime.EventSink) (runtime.Outcome, error) {
		for _, err := range []error{
			sink.ToolCall(runtime.ToolCall{ID: "bash-1", Name: "Bash"}),
			func() error {
				sink.(runtime.ToolExecutionSink).ToolExecutionStarted(runtime.ToolCall{ID: "bash-1", Name: "Bash"})
				return nil
			}(),
			sink.ToolResult(runtime.ToolResult{ID: "bash-1", Name: "Bash", Result: "boom", IsError: true}),
			sink.ToolCall(runtime.ToolCall{ID: "agent-1", Name: "Agent"}),
			sink.ToolCall(runtime.ToolCall{ID: "read-1", Name: "Read", ParentToolID: "agent-1"}),
			sink.ToolResult(runtime.ToolResult{ID: "read-1", Name: "Read", Result: "ok"}),
			sink.ToolResult(runtime.ToolResult{ID: "agent-1", Name: "Agent", Result: "done"}),
		} {
			if err != nil {
				return runtime.Outcome{}, err
			}
		}
		sink.ModelCall(runtime.ModelCall{ParentToolID: "agent-1"})
		sink.ModelCall(runtime.ModelCall{})
		sink.ModelCall(runtime.ModelCall{})
		return runtime.Outcome{}, nil
	}
	executor := newExecutor(t, run, tracing.RuntimeTelemetry{
		Runtime: tracing.RuntimeClaude, AgentName: "reporter", AgentNamespace: "kagent", Provider: "anthropic", Model: "opus",
	})
	for range executor.Execute(t.Context(), requestContext("task-tool-metrics", "hello")) {
	}

	var collected metricdata.ResourceMetrics
	if err := reader.Collect(t.Context(), &collected); err != nil {
		t.Fatal(err)
	}
	toolErrors := map[string]string{}
	calls := map[string]int64{}
	for _, scope := range collected.ScopeMetrics {
		for _, m := range scope.Metrics {
			switch m.Name {
			case "gen_ai.execute_tool.duration":
				for _, point := range m.Data.(metricdata.Histogram[float64]).DataPoints {
					name, _ := point.Attributes.Value("gen_ai.tool.name")
					errorType, _ := point.Attributes.Value(attribute.Key(tracing.AttributeErrorType))
					toolErrors[name.AsString()] = errorType.AsString()
					if !slices.Equal(point.Bounds, durationBuckets) {
						t.Errorf("tool duration bounds = %v, want the advised buckets", point.Bounds)
					}
					// The subagent's tool is not the agent's own, so it has no agent name.
					if name.AsString() == "Read" {
						if point.Attributes.HasValue(attribute.Key(tracing.AttributeAgentName)) {
							t.Error("the subagent's Read tool carries gen_ai.agent.name")
						}
						continue
					}
					wantAgent(t, point.Attributes)
				}
			case "gen_ai.invoke_agent.inference_calls", "gen_ai.invoke_agent.tool_calls":
				points := m.Data.(metricdata.Histogram[int64]).DataPoints
				if len(points) != 1 || points[0].Count != 1 || !slices.Equal(points[0].Bounds, callBuckets) {
					t.Fatalf("%s points = %+v, want one value in the advised buckets", m.Name, points)
				}
				calls[m.Name] = points[0].Sum
				wantAgent(t, points[0].Attributes)
			}
		}
	}
	// Every tool span records a duration, a subagent's tool included.
	if want := map[string]string{"Bash": errorTypeTool, "Agent": "", "Read": ""}; !maps.Equal(toolErrors, want) {
		t.Errorf("tool durations by name and error = %v, want %v", toolErrors, want)
	}
	// The subagent's model call and tool are not counted for the agent.
	if want := map[string]int64{"gen_ai.invoke_agent.inference_calls": 2, "gen_ai.invoke_agent.tool_calls": 2}; !maps.Equal(calls, want) {
		t.Errorf("calls per invocation = %v, want %v", calls, want)
	}
}

func wantAgent(t *testing.T, set attribute.Set) {
	t.Helper()
	if got, _ := set.Value(attribute.Key(tracing.AttributeAgentName)); got.AsString() != "reporter" {
		t.Errorf("gen_ai.agent.name = %q, want reporter", got.AsString())
	}
}

func TestInvocationMetricsSkipARequestTheAgentNeverRan(t *testing.T) {
	for _, tc := range []struct {
		name string
		busy bool // another turn holds the agent, so the request is rejected as busy
	}{
		{name: "invalid request"},
		{name: "busy agent", busy: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			reader := installMetricReader(t)
			started := make(chan struct{})
			run := func(ctx context.Context, _ runtime.Turn, _ runtime.EventSink) (runtime.Outcome, error) {
				close(started)
				<-ctx.Done()
				return runtime.Outcome{}, ctx.Err()
			}
			executor := newExecutor(t, run, tracing.RuntimeTelemetry{})
			request := requestContext("", "hello") // a request without a task ID is invalid
			if tc.busy {
				firstDone := make(chan struct{})
				go func() {
					defer close(firstDone)
					_, _ = collect(executor.Execute(context.Background(), requestContext("task-1", "first")))
				}()
				<-started
				t.Cleanup(func() {
					_, _ = collect(executor.Cancel(context.Background(), requestContext("task-1", "ignored")))
					<-firstDone
				})
				request = requestContext("task-2", "second")
			}
			if _, errs := collect(executor.Execute(t.Context(), request)); len(errs) == 0 {
				t.Fatal("the request was accepted, want it rejected")
			}

			// The first turn of the busy case is still running, so nothing has ended yet.
			var collected metricdata.ResourceMetrics
			if err := reader.Collect(t.Context(), &collected); err != nil {
				t.Fatal(err)
			}
			for _, scope := range collected.ScopeMetrics {
				for _, m := range scope.Metrics {
					if strings.HasPrefix(m.Name, "gen_ai.invoke_agent.") {
						t.Errorf("recorded %s for a request the agent never ran", m.Name)
					}
				}
			}
		})
	}
}

// installMetricReader makes the global meter provider record into the returned reader for one test.
func installMetricReader(t *testing.T) *sdkmetric.ManualReader {
	t.Helper()
	reader := sdkmetric.NewManualReader()
	provider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	previous := otel.GetMeterProvider()
	otel.SetMeterProvider(provider)
	t.Cleanup(func() {
		otel.SetMeterProvider(previous)
		_ = provider.Shutdown(context.Background())
	})
	return reader
}
