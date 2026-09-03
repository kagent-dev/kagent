package a2a

import (
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	adkmodel "google.golang.org/adk/v2/model"
	adksession "google.golang.org/adk/v2/session"
	"google.golang.org/genai"
)

// tokenUsageSeriesCount returns how many label series currently exist on the
// gen_ai_client_token_usage histogram across the default registry.
func tokenUsageSeriesCount(t *testing.T) int {
	t.Helper()
	mfs, err := prometheus.DefaultGatherer.Gather()
	if err != nil {
		t.Fatalf("gather: %v", err)
	}
	for _, mf := range mfs {
		if mf.GetName() != "gen_ai_client_token_usage" {
			continue
		}
		return len(mf.GetMetric())
	}
	return 0
}

func TestRecordTokenUsage_RecordsPerLLMCall(t *testing.T) {
	t.Setenv("OTEL_METRICS_ENABLED", "true")
	series := 0

	// Partial (streaming) events must be skipped: a streamed call emits many
	// partial chunks but usage is reported once on the final non-partial event.
	recordTokenUsage("gpt-4o", "openai", "my-agent", &adksession.Event{
		LLMResponse: adkmodel.LLMResponse{
			Partial:       true,
			ModelVersion:  "gpt-4o-2024-11-20",
			UsageMetadata: &genai.GenerateContentResponseUsageMetadata{PromptTokenCount: 100, CandidatesTokenCount: 42},
		},
	})
	if got := tokenUsageSeriesCount(t); got != series {
		t.Fatalf("partial event must not record tokens, got %d series", got)
	}

	// The aggregated non-partial event records one input and one output series.
	recordTokenUsage("gpt-4o", "openai", "my-agent", &adksession.Event{
		LLMResponse: adkmodel.LLMResponse{
			ModelVersion:  "gpt-4o-2023-11-20",
			UsageMetadata: &genai.GenerateContentResponseUsageMetadata{PromptTokenCount: 100, CandidatesTokenCount: 40, ThoughtsTokenCount: 2},
		},
	})
	got := tokenUsageSeriesCount(t)
	if got != 2 {
		t.Fatalf("expected input+output series after one LLM call, got %d", got)
	}
}
