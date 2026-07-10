package telemetry

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/prometheus/client_golang/prometheus/testutil"
)

// TestRecordTokenUsage_RecordsHistogram verifies input/output token counts are
// recorded as two separate series on the gen_ai_client_token_usage histogram.
func TestRecordTokenUsage_RecordsHistogram(t *testing.T) {
	tokenUsage.Reset()

	RecordTokenUsage(TokenUsage{
		RequestModel: "gpt-4o", Provider: "openai", InputTokens: 100, OutputTokens: 42,
	})

	// One series per token type (input, output).
	if got := testutil.CollectAndCount(tokenUsage); got != 2 {
		t.Fatalf("expected 2 histogram series (input+output), got %d", got)
	}
}

// TestRecordTokenUsage_SkipsZero verifies zero counts produce no series.
func TestRecordTokenUsage_SkipsZero(t *testing.T) {
	tokenUsage.Reset()

	RecordTokenUsage(TokenUsage{RequestModel: "gpt-4o", Provider: "openai"})

	if got := testutil.CollectAndCount(tokenUsage); got != 0 {
		t.Fatalf("expected no series for zero token counts, got %d", got)
	}
}

// TestRecordTokenUsage_ResponseModelFallback verifies response model defaults to
// the request model when unset, and is used when provided.
func TestRecordTokenUsage_ResponseModelFallback(t *testing.T) {
	tokenUsage.Reset()
	RecordTokenUsage(TokenUsage{RequestModel: "gemini-2.5-flash", Provider: "gcp.vertex_ai", InputTokens: 3})
	RecordTokenUsage(TokenUsage{RequestModel: "gemini-2.5-flash", ResponseModel: "gemini-2.5-flash-002", Provider: "gcp.vertex_ai", InputTokens: 3})

	body := serveMetrics(t)
	if !strings.Contains(body, `gen_ai_response_model="gemini-2.5-flash"`) {
		t.Errorf("expected response model to fall back to request model")
	}
	if !strings.Contains(body, `gen_ai_response_model="gemini-2.5-flash-002"`) {
		t.Errorf("expected explicit response model to be used")
	}
}

// TestMetricsHandler_ServesLabels verifies the /metrics handler exposes the
// semconv labels, including error.type and provider/response model.
func TestMetricsHandler_ServesLabels(t *testing.T) {
	tokenUsage.Reset()
	RecordTokenUsage(TokenUsage{
		RequestModel: "claude-3-5-sonnet", ResponseModel: "claude-3-5-sonnet-20241022",
		Provider: "anthropic", ErrorType: "overloaded_error", InputTokens: 10, OutputTokens: 5,
	})

	body := serveMetrics(t)
	for _, want := range []string{
		"gen_ai_client_token_usage_count",
		`gen_ai_token_type="input"`,
		`gen_ai_token_type="output"`,
		`gen_ai_operation_name="chat"`,
		`gen_ai_provider_name="anthropic"`,
		`gen_ai_request_model="claude-3-5-sonnet"`,
		`gen_ai_response_model="claude-3-5-sonnet-20241022"`,
		`error_type="overloaded_error"`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("metrics output missing %q", want)
		}
	}
}

// TestSemconvProviderName verifies kagent model types map to well-known
// gen_ai.provider.name values, with unknown types passing through.
func TestSemconvProviderName(t *testing.T) {
	cases := map[string]string{
		"openai":           "openai",
		"azure_openai":     "azure.ai.openai",
		"anthropic":        "anthropic",
		"gemini":           "gcp.gemini",
		"gemini_vertex_ai": "gcp.vertex_ai",
		"gemini_anthropic": "gcp.vertex_ai",
		"bedrock":          "aws.bedrock",
		"ollama":           "ollama",      // pass-through (no well-known value)
		"sap_ai_core":      "sap_ai_core", // pass-through
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
