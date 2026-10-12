package runner

import (
	"context"
	"iter"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/kagent-dev/kagent/go/api/adk"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/require"
	adkagent "google.golang.org/adk/v2/agent"
	"google.golang.org/adk/v2/agent/llmagent"
	"google.golang.org/adk/v2/model"
	adkrunner "google.golang.org/adk/v2/runner"
	"google.golang.org/adk/v2/session"
	"google.golang.org/genai"
)

type metricsContext struct {
	adkagent.Context
	agentName string
}

var _ adkagent.Context = (*metricsContext)(nil)

func (metrics *metricsContext) AgentName() string { return metrics.agentName }

type tokenHistogram struct {
	labels map[string]string
	count  uint64
	sum    float64
}

func tokenHistograms(t *testing.T, agentName string) map[string]tokenHistogram {
	t.Helper()
	families, err := prometheus.DefaultGatherer.Gather()
	require.NoError(t, err)
	histograms := make(map[string]tokenHistogram)
	for _, family := range families {
		if family.GetName() != "gen_ai_client_token_usage" {
			continue
		}
		for _, metric := range family.Metric {
			labels := make(map[string]string)
			for _, label := range metric.Label {
				labels[label.GetName()] = label.GetValue()
			}
			if labels["gen_ai_agent_name"] == agentName {
				tokenType := labels["gen_ai_token_type"]
				_, duplicate := histograms[tokenType]
				require.False(t, duplicate, "unexpected duplicate token type: %s", tokenType)
				histograms[tokenType] = tokenHistogram{labels: labels, count: metric.Histogram.GetSampleCount(), sum: metric.Histogram.GetSampleSum()}
			}
		}
	}
	return histograms
}

func TestTokenUsagePlugin(t *testing.T) {
	base := adk.BaseModel{Model: "configured-model"}
	for _, test := range []struct {
		name      string
		input     adk.Model
		provider  string
		operation string
	}{
		{name: "openai", input: &adk.OpenAI{BaseModel: base}, provider: "openai", operation: "chat"},
		{name: "gemini", input: &adk.Gemini{BaseModel: base}, provider: "gcp.gemini", operation: "generate_content"},
		{name: "vertex", input: &adk.GeminiVertexAI{BaseModel: base}, provider: "gcp.vertex_ai", operation: "generate_content"},
		{name: "vertex anthropic", input: &adk.GeminiAnthropic{BaseModel: base}, provider: "gcp.vertex_ai", operation: "chat"},
		{name: "bedrock", input: &adk.Bedrock{BaseModel: base}, provider: "aws.bedrock", operation: "chat"},
		{name: "sap", input: &adk.SAPAICore{BaseModel: base}, provider: "sap_ai_core", operation: "chat"},
		{name: "foundry", input: &adk.Foundry{BaseModel: base}, provider: "foundry", operation: "chat"},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Setenv("OTEL_METRICS_ENABLED", "true")
			metricsPlugin, err := newTokenUsagePlugin(test.input)
			require.NoError(t, err)
			t.Cleanup(func() { require.NoError(t, metricsPlugin.Close()) })
			agentName := "metrics_" + strings.ReplaceAll(uuid.NewString(), "-", "_")
			ctx := &metricsContext{agentName: agentName}
			callback := metricsPlugin.AfterModelCallback()
			for _, response := range []*model.LLMResponse{
				nil,
				{},
				{Partial: true, UsageMetadata: &genai.GenerateContentResponseUsageMetadata{PromptTokenCount: 100}},
			} {
				replacement, err := callback(ctx, response, nil)
				require.NoError(t, err)
				require.Nil(t, replacement)
			}
			require.Empty(t, tokenHistograms(t, agentName))
			response := &model.LLMResponse{
				ModelVersion: "served-model",
				ErrorCode:    "overloaded_error",
				UsageMetadata: &genai.GenerateContentResponseUsageMetadata{
					PromptTokenCount: 100, CandidatesTokenCount: 40, ThoughtsTokenCount: 2,
				},
			}
			for range 2 {
				replacement, err := callback(ctx, response, nil)
				require.NoError(t, err)
				require.Nil(t, replacement)
			}
			histograms := tokenHistograms(t, agentName)
			require.Len(t, histograms, 2)
			for tokenType, want := range map[string]float64{"input": 200, "output": 84} {
				histogram := histograms[tokenType]
				require.Equal(t, uint64(2), histogram.count)
				require.Equal(t, want, histogram.sum)
				require.Equal(t, map[string]string{
					"gen_ai_token_type": tokenType, "gen_ai_operation_name": test.operation,
					"gen_ai_provider_name": test.provider, "gen_ai_request_model": base.Model,
					"gen_ai_response_model": "served-model", "gen_ai_agent_name": agentName,
					"error_type": "overloaded_error",
				}, histogram.labels)
			}
		})
	}
}

type metricsModel struct {
	responses []*model.LLMResponse
}

var _ model.LLM = (*metricsModel)(nil)

func (metrics *metricsModel) Name() string { return "configured-model" }

func (metrics *metricsModel) GenerateContent(context.Context, *model.LLMRequest, bool) iter.Seq2[*model.LLMResponse, error] {
	return func(yield func(*model.LLMResponse, error) bool) {
		for _, response := range metrics.responses {
			if !yield(response, nil) {
				return
			}
		}
	}
}

func TestRunnerRecordsTokenUsage(t *testing.T) {
	t.Setenv("OTEL_METRICS_ENABLED", "true")
	t.Setenv("OPENAI_API_KEY", "test-key")
	t.Setenv("KAGENT_PROPAGATE_TOKEN", "false")
	t.Setenv("STS_WELL_KNOWN_URI", "")
	t.Setenv("KAGENT_SKILLS_FOLDER", "")
	usage := &genai.GenerateContentResponseUsageMetadata{PromptTokenCount: 100, CandidatesTokenCount: 40, ThoughtsTokenCount: 2}
	for _, test := range []struct {
		name  string
		input []*model.LLMResponse
	}{
		{name: "usage without content", input: []*model.LLMResponse{{UsageMetadata: usage}}},
		{name: "streamed response", input: []*model.LLMResponse{
			{Partial: true, Content: genai.NewContentFromText("partial", genai.RoleModel), UsageMetadata: usage},
			{Content: genai.NewContentFromText("final", genai.RoleModel), UsageMetadata: usage},
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			agentConfig := &adk.AgentConfig{Model: &adk.OpenAI{BaseModel: adk.BaseModel{Model: "configured-model"}}}
			runnerConfig, err := CreateRunnerConfig(t.Context(), agentConfig, session.InMemoryService(), "metrics-app", nil, nil)
			require.NoError(t, err)
			require.Len(t, runnerConfig.PluginConfig.Plugins, 1)
			t.Cleanup(func() { require.NoError(t, runnerConfig.PluginConfig.Plugins[0].Close()) })
			agentName := "metrics_" + strings.ReplaceAll(uuid.NewString(), "-", "_")
			runnerConfig.Agent, err = llmagent.New(llmagent.Config{
				Name: agentName, Model: &metricsModel{responses: test.input},
			})
			require.NoError(t, err)
			runnerConfig.AutoCreateSession = true
			runtime, err := adkrunner.New(runnerConfig)
			require.NoError(t, err)
			for _, err := range runtime.Run(t.Context(), "user", "session", genai.NewContentFromText("hello", genai.RoleUser), adkagent.RunConfig{StreamingMode: adkagent.StreamingModeSSE}) {
				require.NoError(t, err)
			}
			histograms := tokenHistograms(t, agentName)
			require.Len(t, histograms, 2)
			require.Equal(t, uint64(1), histograms["input"].count)
			require.Equal(t, float64(100), histograms["input"].sum)
			require.Equal(t, uint64(1), histograms["output"].count)
			require.Equal(t, float64(42), histograms["output"].sum)
		})
	}
}
