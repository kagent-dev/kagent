package a2a

import (
	"testing"

	adkmodel "google.golang.org/adk/v2/model"
	adksession "google.golang.org/adk/v2/session"
	"google.golang.org/genai"
)

// TestTokenUsageFromEvent_Values verifies the input/output token accounting:
// output combines candidate + reasoning tokens, and the model/provider/agent
// labels flow through.
func TestTokenUsageFromEvent_Values(t *testing.T) {
	usage := &genai.GenerateContentResponseUsageMetadata{
		PromptTokenCount:     10,
		CandidatesTokenCount: 5,
		ThoughtsTokenCount:   3,
	}
	event := &adksession.Event{
		LLMResponse: adkmodel.LLMResponse{
			ModelVersion:  "gemini-2.5-flash",
			UsageMetadata: usage,
		},
	}

	got, ok := tokenUsageFromEvent(event, "gemini-2.5-flash", "gcp.gemini", "my-agent")
	if !ok {
		t.Fatal("expected usage to be recorded")
	}
	if got.InputTokens != 10 {
		t.Errorf("input = %d, want 10", got.InputTokens)
	}
	if got.OutputTokens != 8 {
		t.Errorf("output = %d, want 8 (candidates 5 + thoughts 3)", got.OutputTokens)
	}
	if got.RequestModel != "gemini-2.5-flash" || got.ProviderName != "gcp.gemini" || got.AgentName != "my-agent" {
		t.Errorf("labels not propagated: %+v", got)
	}
}

// TestTokenUsageFromEvent_SkipsPartialAndNil verifies streamed chunks that carry
// usage metadata are skipped (one observation per LLM call), and events without
// usage produce nothing.
func TestTokenUsageFromEvent_SkipsPartialAndNil(t *testing.T) {
	usage := &genai.GenerateContentResponseUsageMetadata{PromptTokenCount: 10, CandidatesTokenCount: 5}

	if _, ok := tokenUsageFromEvent(
		&adksession.Event{LLMResponse: adkmodel.LLMResponse{Partial: true, UsageMetadata: usage}},
		"m", "p", "a",
	); ok {
		t.Error("partial event must not be recorded")
	}

	if _, ok := tokenUsageFromEvent(&adksession.Event{}, "m", "p", "a"); ok {
		t.Error("event without usage metadata must not be recorded")
	}
}

// TestRecordTokenUsage_NoopWithUninitializedRecorder verifies the executor
// recording path is a no-op before telemetry metrics are initialized (metrics
// disabled), so it cannot panic.
func TestRecordTokenUsage_NoopWithUninitializedRecorder(t *testing.T) {
	usage := &genai.GenerateContentResponseUsageMetadata{PromptTokenCount: 10, CandidatesTokenCount: 5}
	event := &adksession.Event{LLMResponse: adkmodel.LLMResponse{UsageMetadata: usage}}
	recordTokenUsage(t.Context(), event, "gemini-2.5-flash", "gcp.gemini", "my-agent")
}
