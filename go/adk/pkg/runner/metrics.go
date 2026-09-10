package runner

import (
	"github.com/kagent-dev/kagent/go/adk/pkg/config"
	"github.com/kagent-dev/kagent/go/adk/pkg/telemetry"
	"github.com/kagent-dev/kagent/go/api/adk"
	adkagent "google.golang.org/adk/v2/agent"
	"google.golang.org/adk/v2/model"
	"google.golang.org/adk/v2/plugin"
)

func newTokenUsagePlugin(modelConfig adk.Model) (*plugin.Plugin, error) {
	modelName := config.GetModelName(modelConfig)
	providerName := telemetry.SemconvProviderName(modelConfig.GetType())
	operationName := "chat"
	if modelConfig.GetType() == adk.ModelTypeGemini || modelConfig.GetType() == adk.ModelTypeGeminiVertexAI {
		operationName = "generate_content"
	}
	return plugin.New(plugin.Config{
		Name: "kagent_token_usage",
		AfterModelCallback: func(ctx adkagent.Context, response *model.LLMResponse, _ error) (*model.LLMResponse, error) {
			if response == nil || response.Partial || response.UsageMetadata == nil {
				return nil, nil
			}
			usage := response.UsageMetadata
			telemetry.RecordTokenUsage(telemetry.TokenUsage{
				OperationName: operationName,
				RequestModel:  modelName,
				ResponseModel: response.ModelVersion,
				Provider:      providerName,
				AgentName:     ctx.AgentName(),
				ErrorType:     response.ErrorCode,
				InputTokens:   int64(usage.PromptTokenCount),
				OutputTokens:  int64(usage.CandidatesTokenCount) + int64(usage.ThoughtsTokenCount),
			})
			return nil, nil
		},
	})
}
