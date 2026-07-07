package a2a

import (
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	adkmodel "google.golang.org/adk/model"
	adksession "google.golang.org/adk/session"
	"google.golang.org/genai"
)

// gatherTokenUsageCount returns the total number of observations recorded on the
// gen_ai_client_token_usage histogram (summed across all label series).
func gatherTokenUsageCount(t *testing.T) uint64 {
	t.Helper()
	mfs, err := prometheus.DefaultGatherer.Gather()
	if err != nil {
		t.Fatalf("gather: %v", err)
	}
	var total uint64
	for _, mf := range mfs {
		if mf.GetName() != "gen_ai_client_token_usage" {
			continue
		}
		for _, m := range mf.GetMetric() {
			total += m.GetHistogram().GetSampleCount()
		}
	}
	return total
}

// TestExecutor_StreamingUsageNotDoubleCounted verifies that streamed LLM calls
// record one input + one output observation per call, not one per stream chunk.
// Partial events are skipped even when they carry usage metadata.
func TestExecutor_StreamingUsageNotDoubleCounted(t *testing.T) {
	e := &KAgentExecutor{modelName: "gemini-2.5-flash", providerName: "gcp.vertex_ai"}
	usage := &genai.GenerateContentResponseUsageMetadata{PromptTokenCount: 10, CandidatesTokenCount: 5}

	before := gatherTokenUsageCount(t)

	const numCalls, chunksPerCall = 3, 4
	for range numCalls {
		// Streaming partial chunks that also carry usage — must be skipped.
		for range chunksPerCall {
			e.recordTokenUsage(&adksession.Event{LLMResponse: adkmodel.LLMResponse{Partial: true, UsageMetadata: usage}})
		}
		// Final aggregated (non-partial) event — the one that counts.
		e.recordTokenUsage(&adksession.Event{LLMResponse: adkmodel.LLMResponse{UsageMetadata: usage}})
	}

	// One input + one output observation per LLM call, regardless of chunk count.
	if got, want := gatherTokenUsageCount(t)-before, uint64(numCalls*2); got != want {
		t.Fatalf("token usage observations = %d, want %d (must count %d LLM calls, not %d stream chunks)",
			got, want, numCalls, numCalls*(chunksPerCall+1))
	}
}
