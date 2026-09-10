package telemetry

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
	dto "github.com/prometheus/client_model/go"
	"github.com/stretchr/testify/require"
)

// TestRecordTokenUsage_RecordsHistogram verifies input/output token counts are
// recorded as two separate series on the gen_ai_client_token_usage histogram.
func TestRecordTokenUsage_RecordsHistogram(t *testing.T) {
	t.Setenv(metricsEnabledEnvVar, "true")
	tokenUsage.Reset()

	RecordTokenUsage(TokenUsage{
		RequestModel: "gpt-4o", Provider: "openai", InputTokens: 100, OutputTokens: 42,
	})

	// One series per token type (input, output).
	if got := testutil.CollectAndCount(tokenUsage); got != 2 {
		t.Fatalf("expected 2 histogram series (input+output), got %d", got)
	}
}

func TestRecordTokenUsage_SkipsZero(t *testing.T) {
	t.Setenv(metricsEnabledEnvVar, "true")
	tokenUsage.Reset()

	RecordTokenUsage(TokenUsage{RequestModel: "gpt-4o", Provider: "openai"})

	if got := testutil.CollectAndCount(tokenUsage); got != 0 {
		t.Fatalf("expected no series for zero token counts, got %d", got)
	}
}

func TestRecordTokenUsage_Disabled(t *testing.T) {
	t.Setenv(metricsEnabledEnvVar, "false")
	tokenUsage.Reset()

	RecordTokenUsage(TokenUsage{RequestModel: "gpt-4o", Provider: "openai", InputTokens: 100})

	if got := testutil.CollectAndCount(tokenUsage); got != 0 {
		t.Fatalf("expected no series when metrics are disabled, got %d", got)
	}
}

func TestRecordTokenUsage_ResponseModelFallback(t *testing.T) {
	t.Setenv(metricsEnabledEnvVar, "true")
	tokenUsage.Reset()
	RecordTokenUsage(TokenUsage{RequestModel: "gemini-2.5-flash", Provider: "gcp.vertex_ai", InputTokens: 3})
	RecordTokenUsage(TokenUsage{RequestModel: "gemini-2.5-flash", ResponseModel: "gemini-2.5-flash-002", Provider: "gcp.vertex_ai", InputTokens: 3})

	body := serveMetrics(t)
	if !strings.Contains(body, "gen_ai_response_model=\"gemini-2.5-flash\"") {
		t.Errorf("expected response model to fall back to request model")
	}
	if !strings.Contains(body, "gen_ai_response_model=\"gemini-2.5-flash-002\"") {
		t.Errorf("expected explicit response model to be used")
	}
}

func TestMetricsHandler_ServesLabels(t *testing.T) {
	t.Setenv(metricsEnabledEnvVar, "true")
	tokenUsage.Reset()
	RecordTokenUsage(TokenUsage{
		RequestModel: "claude-3-5-sonnet", ResponseModel: "claude-3-5-sonnet-20241022",
		Provider: "anthropic", AgentName: "my-agent", ErrorType: "overloaded_error",
		InputTokens: 10, OutputTokens: 5,
	})

	body := serveMetrics(t)
	for _, want := range []string{
		"gen_ai_client_token_usage_count",
		"gen_ai_token_type=\"input\"",
		"gen_ai_token_type=\"output\"",
		"gen_ai_operation_name=\"chat\"",
		"gen_ai_provider_name=\"anthropic\"",
		"gen_ai_request_model=\"claude-3-5-sonnet\"",
		"gen_ai_response_model=\"claude-3-5-sonnet-20241022\"",
		"gen_ai_agent_name=\"my-agent\"",
		"error_type=\"overloaded_error\"",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("metrics output missing %q", want)
		}
	}
}

func TestSemconvProviderName(t *testing.T) {
	cases := map[string]string{
		"openai":           "openai",
		"azure_openai":     "azure.ai.openai",
		"anthropic":        "anthropic",
		"gemini":           "gcp.gemini",
		"gemini_vertex_ai": "gcp.vertex_ai",
		"gemini_anthropic": "gcp.vertex_ai",
		"bedrock":          "aws.bedrock",
		"ollama":           "ollama",
		"sap_ai_core":      "sap_ai_core",
		"some-custom":      "some-custom",
	}
	for input, want := range cases {
		t.Run(input, func(t *testing.T) {
			require.Equal(t, want, SemconvProviderName(input))
		})
	}
}

func TestRecordTokenUsageValues(t *testing.T) {
	for _, test := range []struct {
		name  string
		input TokenUsage
		want  map[string]float64
	}{
		{name: "input and output", input: TokenUsage{InputTokens: 100, OutputTokens: 42}, want: map[string]float64{"input": 100, "output": 42}},
		{name: "input only", input: TokenUsage{InputTokens: 100}, want: map[string]float64{"input": 100}},
		{name: "output only", input: TokenUsage{OutputTokens: 42}, want: map[string]float64{"output": 42}},
		{name: "zero", input: TokenUsage{}},
		{name: "negative", input: TokenUsage{InputTokens: -1, OutputTokens: -2}},
		{name: "negative input", input: TokenUsage{InputTokens: -1, OutputTokens: 42}, want: map[string]float64{"output": 42}},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Setenv(metricsEnabledEnvVar, "true")
			tokenUsage.Reset()
			t.Cleanup(tokenUsage.Reset)
			RecordTokenUsage(test.input)
			RecordTokenUsage(test.input)
			require.Equal(t, len(test.want), testutil.CollectAndCount(tokenUsage))
			for tokenType, want := range test.want {
				metric := &dto.Metric{}
				histogram := tokenUsage.WithLabelValues(tokenType, operationChat, "", "", "", "", "")
				require.NoError(t, histogram.(prometheus.Metric).Write(metric))
				require.Equal(t, uint64(2), metric.Histogram.GetSampleCount())
				require.Equal(t, 2*want, metric.Histogram.GetSampleSum())
				require.Len(t, metric.Histogram.Bucket, len(tokenUsageBuckets))
				for index, bucket := range metric.Histogram.Bucket {
					require.Equal(t, tokenUsageBuckets[index], bucket.GetUpperBound())
					var count uint64
					if want <= bucket.GetUpperBound() {
						count = 2
					}
					require.Equal(t, count, bucket.GetCumulativeCount())
				}
			}
		})
	}
}

func TestRecordTokenUsageConcurrent(t *testing.T) {
	t.Setenv(metricsEnabledEnvVar, "true")
	tokenUsage.Reset()
	t.Cleanup(tokenUsage.Reset)
	var workers sync.WaitGroup
	for range 16 {
		workers.Go(func() {
			for range 64 {
				RecordTokenUsage(TokenUsage{InputTokens: 3, OutputTokens: 7})
			}
		})
	}
	for range 16 {
		require.NotEmpty(t, serveMetrics(t))
	}
	workers.Wait()
	for tokenType, want := range map[string]float64{"input": 3, "output": 7} {
		metric := &dto.Metric{}
		histogram := tokenUsage.WithLabelValues(tokenType, operationChat, "", "", "", "", "")
		require.NoError(t, histogram.(prometheus.Metric).Write(metric))
		require.Equal(t, uint64(16*64), metric.Histogram.GetSampleCount())
		require.Equal(t, 16*64*want, metric.Histogram.GetSampleSum())
	}
}

func TestMetricsEnabled(t *testing.T) {
	for _, test := range []struct {
		name  string
		input string
		want  bool
	}{
		{name: "unset"},
		{name: "enabled", input: "true", want: true},
		{name: "case and whitespace", input: " TRUE ", want: true},
		{name: "disabled", input: "false"},
		{name: "invalid", input: "1"},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Setenv(metricsEnabledEnvVar, test.input)
			require.Equal(t, test.want, MetricsEnabled())
		})
	}
}

func serveMetrics(t *testing.T) string {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	rec := httptest.NewRecorder()
	MetricsHandler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	return rec.Body.String()
}
