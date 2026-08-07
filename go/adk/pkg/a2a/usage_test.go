package a2a

import (
	"math"
	"testing"

	a2atype "github.com/a2aproject/a2a-go/a2a"
	"github.com/stretchr/testify/require"
	"google.golang.org/adk/v2/model"
	"google.golang.org/adk/v2/server/adka2a" //nolint:staticcheck // kagent still uses a2a-go v1; this ADK package is the compatibility adapter.
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

func stampedTotal(t *testing.T, usage *turnUsage) map[string]any {
	t.Helper()
	meta := map[string]any{}
	usage.stamp(meta)
	total, ok := meta[GetKAgentMetadataKey("usage_total")].(map[string]any)
	require.True(t, ok, "kagent_usage_total must be stamped")
	return total
}

func TestTurnUsageAggregatesNonPartialEvents(t *testing.T) {
	var usage turnUsage
	require.True(t, usage.empty())

	usage.add(usageEvent(100, 20, 120, "model-a", false))
	usage.add(usageEvent(200, 30, 230, "model-b", false))

	require.False(t, usage.empty())
	require.Equal(t, map[string]any{
		"promptTokenCount":     float64(300),
		"candidatesTokenCount": float64(50),
		"totalTokenCount":      float64(350),
		"modelVersion":         "model-b",
	}, stampedTotal(t, &usage))
}

func TestTurnUsageSkipsPartialAndEmptyEvents(t *testing.T) {
	var usage turnUsage

	usage.add(nil)
	usage.add(&adksession.Event{})
	usage.add(usageEvent(999, 999, 999, "chunk-model", true))

	require.True(t, usage.empty())
	meta := map[string]any{}
	usage.stamp(meta)
	require.NotContains(t, meta, GetKAgentMetadataKey("usage_total"),
		"empty usage must not stamp the key")

	usage.add(usageEvent(10, 5, 15, "", false))
	require.Equal(t, map[string]any{
		"promptTokenCount":     float64(10),
		"candidatesTokenCount": float64(5),
		"totalTokenCount":      float64(15),
	}, stampedTotal(t, &usage), "modelVersion must be omitted when no event carried one")
}

func TestTurnUsageShapeMatchesPerEventUsageMetadata(t *testing.T) {
	event := usageEvent(10, 5, 15, "", false)

	var usage turnUsage
	usage.add(event)

	perEventMeta := buildEventMeta(map[string]any{}, event)
	require.Equal(t,
		perEventMeta[adka2a.ToA2AMetaKey("usage_metadata")],
		stampedTotal(t, &usage),
		"kagent_usage_total must serialize identically to adk_usage_metadata")
}

func TestTurnUsageSeedFromTaskAccumulatesAcrossExecutions(t *testing.T) {
	var usage turnUsage
	// float64 values mimic a JSON round-trip through the task store.
	usage.seedFromTask(&a2atype.Task{Metadata: map[string]any{
		GetKAgentMetadataKey("usage_total"): map[string]any{
			"promptTokenCount":     float64(100),
			"candidatesTokenCount": float64(20),
			"totalTokenCount":      float64(120),
			"modelVersion":         "model-a",
		},
	}})
	usage.add(usageEvent(200, 30, 230, "", false))

	require.Equal(t, map[string]any{
		"promptTokenCount":     float64(300),
		"candidatesTokenCount": float64(50),
		"totalTokenCount":      float64(350),
		"modelVersion":         "model-a",
	}, stampedTotal(t, &usage))
}

func TestTurnUsageSeedFromTaskIgnoresMissingOrMalformed(t *testing.T) {
	var usage turnUsage
	usage.seedFromTask(nil)
	usage.seedFromTask(&a2atype.Task{})
	usage.seedFromTask(&a2atype.Task{Metadata: map[string]any{
		GetKAgentMetadataKey("usage_total"): "not-a-map",
	}})
	require.True(t, usage.empty())
}

func TestClampInt32(t *testing.T) {
	require.Equal(t, int32(42), clampInt32(42))
	require.Equal(t, int32(math.MaxInt32), clampInt32(math.MaxInt32+1))
}
