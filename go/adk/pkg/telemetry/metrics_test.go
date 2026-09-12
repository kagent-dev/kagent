package telemetry

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/require"
)

func TestRecordTokenUsage_RecordsCachedSeries(t *testing.T) {
	t.Setenv(metricsEnabledEnvVar, "true")
	cases := []struct {
		name              string
		input             TokenUsage
		wantResponseModel string
	}{
		{
			name: "response model fallback",
			input: TokenUsage{
				RequestModel: "gpt-4o", Provider: "openai", AgentName: "openai-agent",
				InputTokens: 100, OutputTokens: 42, CachedTokens: 30,
			},
			wantResponseModel: "gpt-4o",
		},
		{
			name: "explicit response model and error",
			input: TokenUsage{
				RequestModel: "claude-sonnet-4", ResponseModel: "claude-sonnet-4-20250514",
				Provider: "anthropic", AgentName: "anthropic-agent", ErrorType: "overloaded_error",
				InputTokens: 200, OutputTokens: 12, CachedTokens: 75,
			},
			wantResponseModel: "claude-sonnet-4-20250514",
		},
		{
			name: "cached only",
			input: TokenUsage{
				RequestModel: "gemini-2.5-flash", Provider: "gcp.vertex_ai", AgentName: "gemini-agent",
				CachedTokens: 10,
			},
			wantResponseModel: "gemini-2.5-flash",
		},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			tokenUsage.Reset()
			t.Cleanup(tokenUsage.Reset)
			registry := prometheus.NewPedanticRegistry()
			registry.MustRegister(tokenUsage)

			const workers = 8
			const callsPerWorker = 10
			var workersDone sync.WaitGroup
			for range workers {
				workersDone.Go(func() {
					for range callsPerWorker {
						RecordTokenUsage(testCase.input)
					}
				})
			}
			workersDone.Wait()

			families, err := registry.Gather()
			require.NoError(t, err)
			require.Len(t, families, 1)
			require.Equal(t, metricGenAIClientTokenUsage, families[0].GetName())
			wantTokens := map[string]int64{tokenTypeCached: testCase.input.CachedTokens}
			if testCase.input.InputTokens > 0 {
				wantTokens[tokenTypeInput] = testCase.input.InputTokens
			}
			if testCase.input.OutputTokens > 0 {
				wantTokens[tokenTypeOutput] = testCase.input.OutputTokens
			}
			require.Len(t, families[0].GetMetric(), len(wantTokens))
			for _, series := range families[0].GetMetric() {
				labels := make(map[string]string)
				for _, label := range series.GetLabel() {
					labels[label.GetName()] = label.GetValue()
				}
				tokenType := labels[labelGenAITokenType]
				require.Contains(t, wantTokens, tokenType)
				require.Equal(t, map[string]string{
					labelGenAITokenType: tokenType, labelGenAIOperationName: operationChat,
					labelGenAIProviderName: testCase.input.Provider, labelGenAIRequestModel: testCase.input.RequestModel,
					labelGenAIResponseModel: testCase.wantResponseModel, labelGenAIAgentName: testCase.input.AgentName,
					labelErrorType: testCase.input.ErrorType,
				}, labels)
				require.NotNil(t, series.Histogram)
				require.Equal(t, uint64(workers*callsPerWorker), series.GetHistogram().GetSampleCount())
				require.Equal(t, float64(workers*callsPerWorker*wantTokens[tokenType]), series.GetHistogram().GetSampleSum())
				delete(wantTokens, tokenType)
			}
			require.Empty(t, wantTokens)
		})
	}
}

func TestRecordTokenUsage_SkipsNonPositiveCached(t *testing.T) {
	t.Setenv(metricsEnabledEnvVar, "true")
	tokenUsage.Reset()

	// Zero cached tokens emits no cached series.
	RecordTokenUsage(TokenUsage{RequestModel: "gpt-4o", Provider: "openai", CachedTokens: 0, InputTokens: 10})
	// Negative cached tokens also emit no cached series.
	RecordTokenUsage(TokenUsage{RequestModel: "gpt-4o", Provider: "openai", CachedTokens: -5, InputTokens: 10})

	body := serveMetrics(t)
	if strings.Contains(body, "gen_ai_token_type=\"cached\"") {
		t.Fatalf("expected no cached series for non-positive CachedTokens")
	}
}

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

	RecordTokenUsage(TokenUsage{RequestModel: "gpt-4o", Provider: "openai", InputTokens: 100, OutputTokens: 42, CachedTokens: 30})

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
	for in, want := range cases {
		if got := SemconvProviderName(in); got != want {
			t.Errorf("SemconvProviderName(%q) = %q, want %q", in, got, want)
		}
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
