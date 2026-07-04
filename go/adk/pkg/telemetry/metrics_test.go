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

	RecordTokenUsage("gpt-4o", "openai", 100, 42)

	// One series per token type (input, output).
	if got := testutil.CollectAndCount(tokenUsage); got != 2 {
		t.Fatalf("expected 2 histogram series (input+output), got %d", got)
	}
}

// TestRecordTokenUsage_SkipsZero verifies zero counts produce no series.
func TestRecordTokenUsage_SkipsZero(t *testing.T) {
	tokenUsage.Reset()

	RecordTokenUsage("gpt-4o", "openai", 0, 0)

	if got := testutil.CollectAndCount(tokenUsage); got != 0 {
		t.Fatalf("expected no series for zero token counts, got %d", got)
	}
}

// TestMetricsHandler_ServesTokenUsage verifies the /metrics handler exposes the
// recorded gen_ai_client_token_usage series with the expected semconv labels in
// the Prometheus text format.
func TestMetricsHandler_ServesTokenUsage(t *testing.T) {
	tokenUsage.Reset()
	RecordTokenUsage("claude-3-5-sonnet", "anthropic", 10, 5)

	req := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	rec := httptest.NewRecorder()
	MetricsHandler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	body := rec.Body.String()
	for _, want := range []string{
		"gen_ai_client_token_usage_count",
		`gen_ai_token_type="input"`,
		`gen_ai_token_type="output"`,
		`gen_ai_request_model="claude-3-5-sonnet"`,
		`gen_ai_provider_name="anthropic"`,
		`gen_ai_operation_name="chat"`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("metrics output missing %q", want)
		}
	}
}
