package runner

import (
	"github.com/kagent-dev/kagent/go/adk/pkg/config"
	"github.com/kagent-dev/kagent/go/adk/pkg/telemetry"
	configv1 "github.com/kagent-dev/kagent/go/api/gen/kagent/config/v1"
	adkagent "google.golang.org/adk/v2/agent"
	"google.golang.org/adk/v2/model"
	"google.golang.org/adk/v2/plugin"
)

func newTokenUsagePlugin(modelConfig *configv1.ADKModelConfig) (*plugin.Plugin, error) {
	modelName := modelConfig.GetModel()
	modelType := config.ModelType(modelConfig)
	providerName := telemetry.SemconvProviderName(modelType)
	operationName := "chat"
	if modelType == "gemini" || modelType == "gemini_vertex_ai" {
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
