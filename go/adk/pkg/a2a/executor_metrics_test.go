package a2a

import (
	"maps"
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/require"
	adkmodel "google.golang.org/adk/v2/model"
	adksession "google.golang.org/adk/v2/session"
	"google.golang.org/genai"
)

type tokenUsageObservation struct {
	count uint64
	sum   float64
}

func tokenUsageForAgent(t *testing.T, agentName string) map[string]tokenUsageObservation {
	t.Helper()
	families, err := prometheus.DefaultGatherer.Gather()
	require.NoError(t, err)
	observations := make(map[string]tokenUsageObservation)
	for _, family := range families {
		if family.GetName() != "gen_ai_client_token_usage" {
			continue
		}
		for _, series := range family.GetMetric() {
			labels := make(map[string]string)
			for _, label := range series.GetLabel() {
				labels[label.GetName()] = label.GetValue()
			}
			if labels["gen_ai_agent_name"] != agentName {
				continue
			}
			tokenType := labels["gen_ai_token_type"]
			require.Equal(t, map[string]string{
				"gen_ai_token_type": tokenType, "gen_ai_operation_name": "chat",
				"gen_ai_provider_name": "openai", "gen_ai_request_model": "gpt-4o",
				"gen_ai_response_model": "gpt-4o-2024-11-20", "gen_ai_agent_name": agentName,
				"error_type": "",
			}, labels)
			require.NotContains(t, observations, tokenType)
			require.NotNil(t, series.Histogram)
			observations[tokenType] = tokenUsageObservation{
				count: series.GetHistogram().GetSampleCount(),
				sum:   series.GetHistogram().GetSampleSum(),
			}
		}
	}
	return observations
}

func TestRecordTokenUsage_RecordsPerLLMCall(t *testing.T) {
	t.Setenv("OTEL_METRICS_ENABLED", "true")
	agentName := t.Name()
	before := tokenUsageForAgent(t, agentName)

	recordTokenUsage("gpt-4o", "openai", agentName, nil)
	recordTokenUsage("gpt-4o", "openai", agentName, &adksession.Event{})
	recordTokenUsage("gpt-4o", "openai", agentName, &adksession.Event{
		LLMResponse: adkmodel.LLMResponse{
			Partial:      true,
			ModelVersion: "gpt-4o-2024-11-20",
			UsageMetadata: &genai.GenerateContentResponseUsageMetadata{
				PromptTokenCount: 100, CandidatesTokenCount: 40, ThoughtsTokenCount: 2, CachedContentTokenCount: 30,
			},
		},
	})
	require.Equal(t, before, tokenUsageForAgent(t, agentName))

	recordTokenUsage("gpt-4o", "openai", agentName, &adksession.Event{
		LLMResponse: adkmodel.LLMResponse{
			ModelVersion:  "gpt-4o-2024-11-20",
			UsageMetadata: &genai.GenerateContentResponseUsageMetadata{PromptTokenCount: 100, CandidatesTokenCount: 40, ThoughtsTokenCount: 2},
		},
	})
	want := maps.Clone(before)
	want["input"] = tokenUsageObservation{count: before["input"].count + 1, sum: before["input"].sum + 100}
	want["output"] = tokenUsageObservation{count: before["output"].count + 1, sum: before["output"].sum + 42}
	require.Equal(t, want, tokenUsageForAgent(t, agentName))

	recordTokenUsage("gpt-4o", "openai", agentName, &adksession.Event{
		LLMResponse: adkmodel.LLMResponse{
			ModelVersion: "gpt-4o-2024-11-20",
			UsageMetadata: &genai.GenerateContentResponseUsageMetadata{
				PromptTokenCount: 100, CandidatesTokenCount: 40, CachedContentTokenCount: 30,
			},
		},
	})
	want["input"] = tokenUsageObservation{count: before["input"].count + 2, sum: before["input"].sum + 200}
	want["output"] = tokenUsageObservation{count: before["output"].count + 2, sum: before["output"].sum + 82}
	want["cached"] = tokenUsageObservation{count: before["cached"].count + 1, sum: before["cached"].sum + 30}
	require.Equal(t, want, tokenUsageForAgent(t, agentName))
}
