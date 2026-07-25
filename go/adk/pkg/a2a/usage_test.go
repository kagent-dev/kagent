package a2a

import (
	"testing"

	"github.com/stretchr/testify/require"
	"google.golang.org/adk/v2/model"
	adksession "google.golang.org/adk/v2/session"
	"google.golang.org/genai"
)

func usageEvent(prompt, completion, total int32, modelVersion string, partial bool) *adksession.Event {
	return &adksession.Event{
		LLMResponse: model.LLMResponse{
			UsageMetadata: &genai.GenerateContentResponseUsageMetadata{
				PromptTokenCount:     prompt,
				CandidatesTokenCount: completion,
				TotalTokenCount:      total,
			},
			ModelVersion: modelVersion,
			Partial:      partial,
		},
	}
}

func TestTurnUsageAggregatesNonPartialEvents(t *testing.T) {
	var usage turnUsage
	require.True(t, usage.empty())

	usage.add(usageEvent(100, 20, 120, "model-a", false))
	usage.add(usageEvent(200, 30, 230, "model-b", false))

	require.False(t, usage.empty())
	require.Equal(t, map[string]any{
		"prompt_tokens":     int64(300),
		"completion_tokens": int64(50),
		"total_tokens":      int64(350),
		"model":             "model-b",
	}, usage.toMetadata())
}

func TestTurnUsageSkipsPartialAndEmptyEvents(t *testing.T) {
	var usage turnUsage

	usage.add(nil)
	usage.add(&adksession.Event{})
	usage.add(usageEvent(999, 999, 999, "chunk-model", true))

	require.True(t, usage.empty())

	usage.add(usageEvent(10, 5, 15, "", false))
	require.Equal(t, map[string]any{
		"prompt_tokens":     int64(10),
		"completion_tokens": int64(5),
		"total_tokens":      int64(15),
	}, usage.toMetadata(), "model key must be omitted when no event carried a model version")
}
