package telemetry

import (
	"context"
	"testing"

	"go.opentelemetry.io/otel/attribute"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
)

// withTokenRecorder installs a manual meter reader so tests can inspect the
// gen_ai.client.token.usage histogram after RecordTokenUsage calls.
func withTokenRecorder(t *testing.T) *sdkmetric.ManualReader {
	t.Helper()
	prev := tokenUsageHistogram
	reader := sdkmetric.NewManualReader()
	mp := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	initTokenUsageRecorder(mp)
	t.Cleanup(func() {
		tokenUsageHistogram = prev
		_ = mp.Shutdown(context.Background())
	})
	return reader
}

// tokenUsagePoint returns the unique data point on gen_ai.client.token.usage
// whose attributes carry the given token type, failing the test otherwise.
func tokenUsagePoint(t *testing.T, reader *sdkmetric.ManualReader, tokenType string) metricdata.HistogramDataPoint[int64] {
	t.Helper()
	var rm metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &rm); err != nil {
		t.Fatalf("collect: %v", err)
	}
	for _, sm := range rm.ScopeMetrics {
		for _, m := range sm.Metrics {
			if m.Name != metricGenAIClientTokenUsage {
				continue
			}
			hist, ok := m.Data.(metricdata.Histogram[int64])
			if !ok {
				t.Fatalf("expected histogram data, got %T", m.Data)
			}
			for _, dp := range hist.DataPoints {
				if v, _ := dp.Attributes.Value(attribute.Key(attrGenAITokenType)); v.AsString() == tokenType {
					return dp
				}
			}
		}
	}
	t.Fatalf("no data point with gen_ai.token.type=%q on %s", tokenType, metricGenAIClientTokenUsage)
	return metricdata.HistogramDataPoint[int64]{}
}

func attrString(t *testing.T, dp metricdata.HistogramDataPoint[int64], key string) string {
	t.Helper()
	v, ok := dp.Attributes.Value(attribute.Key(key))
	if !ok {
		t.Fatalf("attribute %q not set on data point", key)
	}
	s := v.AsString()
	if s == "" {
		t.Fatalf("attribute %q empty on data point", key)
	}
	return s
}

func TestRecordTokenUsage_RecordsInputAndOutput(t *testing.T) {
	reader := withTokenRecorder(t)

	RecordTokenUsage(context.Background(), TokenUsage{
		RequestModel:  "gpt-4o",
		ResponseModel: "gpt-4o-2024-05-13",
		ProviderName:  "openai",
		AgentName:     "my-agent",
		InputTokens:   10,
		OutputTokens:  5,
	})

	input := tokenUsagePoint(t, reader, tokenTypeInput)
	if got := input.Count; got != 1 {
		t.Fatalf("input count = %d, want 1", got)
	}
	if got := input.Sum; got != 10 {
		t.Fatalf("input sum = %d, want 10", got)
	}
	if got := attrString(t, input, attrGenAIRequestModel); got != "gpt-4o" {
		t.Errorf("input request model = %q", got)
	}
	if got := attrString(t, input, attrGenAIResponseModel); got != "gpt-4o-2024-05-13" {
		t.Errorf("input response model = %q", got)
	}
	if got := attrString(t, input, attrGenAIProviderName); got != "openai" {
		t.Errorf("input provider = %q", got)
	}
	if got := attrString(t, input, attrGenAIAgentName); got != "my-agent" {
		t.Errorf("input agent = %q", got)
	}

	output := tokenUsagePoint(t, reader, tokenTypeOutput)
	if got := output.Sum; got != 5 {
		t.Fatalf("output sum = %d, want 5", got)
	}
	if got := output.Count; got != 1 {
		t.Fatalf("output count = %d, want 1", got)
	}
}

func TestRecordTokenUsage_ResponseModelFallsBackToRequest(t *testing.T) {
	reader := withTokenRecorder(t)

	RecordTokenUsage(context.Background(), TokenUsage{
		RequestModel: "gemini-2.5-flash",
		ProviderName: "gcp.gemini",
		AgentName:    "a",
		InputTokens:  3,
	})

	pt := tokenUsagePoint(t, reader, tokenTypeInput)
	if got := attrString(t, pt, attrGenAIResponseModel); got != "gemini-2.5-flash" {
		t.Errorf("response model = %q, want fallback to request model", got)
	}
}

func TestRecordTokenUsage_SkipsZeroAndNegative(t *testing.T) {
	reader := withTokenRecorder(t)

	// All-zero, and negative/zero mixes, must not create data points.
	RecordTokenUsage(context.Background(), TokenUsage{
		RequestModel: "gpt-4o", ProviderName: "openai", AgentName: "my-agent",
	})
	RecordTokenUsage(context.Background(), TokenUsage{
		RequestModel: "gpt-4o", ProviderName: "openai", InputTokens: -1,
	})

	var rm metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &rm); err != nil {
		t.Fatalf("collect: %v", err)
	}
	for _, sm := range rm.ScopeMetrics {
		for _, m := range sm.Metrics {
			if m.Name == metricGenAIClientTokenUsage {
				t.Fatalf("expected no data on %s for zero/negative counts", metricGenAIClientTokenUsage)
			}
		}
	}
}

func TestRecordTokenUsage_NoopWhenNotInitialized(t *testing.T) {
	prev := tokenUsageHistogram
	tokenUsageHistogram = nil
	t.Cleanup(func() { tokenUsageHistogram = prev })

	RecordTokenUsage(context.Background(), TokenUsage{
		RequestModel: "gpt-4o", ProviderName: "openai", InputTokens: 10, OutputTokens: 5,
	}) // must not panic when metrics are disabled
}

func TestSemconvProviderName(t *testing.T) {
	cases := map[string]string{
		"openai":           "openai",
		"azure_openai":     "azure.ai.openai",
		"anthropic":        "anthropic",
		"gemini":           "gcp.gemini",
		"gemini_vertex_ai": "gcp.gemini",
		"gemini_anthropic": "gcp.gemini",
		"bedrock":          "aws.bedrock",
		"ollama":           "ollama",
		"some-custom":      "some-custom",
	}
	for in, want := range cases {
		if got := SemconvProviderName(in); got != want {
			t.Errorf("SemconvProviderName(%q) = %q, want %q", in, got, want)
		}
	}
}
