package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/go-logr/logr"
	"github.com/kagent-dev/kagent/go/adk/pkg/config"
	"github.com/kagent-dev/kagent/go/adk/pkg/mcp"
	"github.com/kagent-dev/kagent/go/adk/pkg/models"
	"google.golang.org/adk/agent"
	"google.golang.org/adk/agent/llmagent"
	adkmodel "google.golang.org/adk/model"
	adkgemini "google.golang.org/adk/model/gemini"
	"google.golang.org/adk/tool"
	"google.golang.org/genai"
)

// CreateGoogleADKAgent creates a Google ADK agent from AgentConfig.
// Toolsets are passed in directly (created by mcp.CreateToolsets).
// agentName is used as the ADK agent identity (appears in event Author field).
func CreateGoogleADKAgent(ctx context.Context, agentConfig *config.AgentConfig, agentName string) (agent.Agent, error) {
	log := logr.FromContextOrDiscard(ctx)

	if agentConfig == nil {
		return nil, fmt.Errorf("agent config is required")
	}

	toolsets := mcp.CreateToolsets(ctx, agentConfig.HttpTools, agentConfig.SseTools)

	if agentConfig.Model == nil {
		return nil, fmt.Errorf("model configuration is required")
	}

	llmModel, err := createLLM(ctx, agentConfig.Model, log)
	if err != nil {
		return nil, fmt.Errorf("failed to create LLM: %w", err)
	}

	if agentName == "" {
		agentName = "agent"
	}

	llmAgentConfig := llmagent.Config{
		Name:            agentName,
		Description:     agentConfig.Description,
		Instruction:     agentConfig.Instruction,
		Model:           llmModel,
		IncludeContents: llmagent.IncludeContentsDefault,
		Toolsets:        toolsets,
		BeforeToolCallbacks: []llmagent.BeforeToolCallback{
			makeBeforeToolCallback(log),
		},
		AfterToolCallbacks: []llmagent.AfterToolCallback{
			makeAfterToolCallback(log),
		},
		OnToolErrorCallbacks: []llmagent.OnToolErrorCallback{
			makeOnToolErrorCallback(log),
		},
	}

	log.Info("Creating Google ADK LLM agent",
		"name", llmAgentConfig.Name,
		"hasDescription", llmAgentConfig.Description != "",
		"hasInstruction", llmAgentConfig.Instruction != "",
		"toolsetsCount", len(llmAgentConfig.Toolsets))

	llmAgent, err := llmagent.New(llmAgentConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to create LLM agent: %w", err)
	}

	log.Info("Successfully created Google ADK LLM agent", "toolsetsCount", len(llmAgentConfig.Toolsets))

	return llmAgent, nil
}

// createLLM creates an adkmodel.LLM from the model configuration.
func createLLM(ctx context.Context, m config.Model, log logr.Logger) (adkmodel.LLM, error) {
	switch m := m.(type) {
	case *config.OpenAI:
		cfg := &models.OpenAIConfig{
			Model:            m.Model,
			BaseUrl:          m.BaseUrl,
			Headers:          extractHeaders(m.Headers),
			FrequencyPenalty: m.FrequencyPenalty,
			MaxTokens:        m.MaxTokens,
			N:                m.N,
			PresencePenalty:  m.PresencePenalty,
			ReasoningEffort:  m.ReasoningEffort,
			Seed:             m.Seed,
			Temperature:      m.Temperature,
			Timeout:          m.Timeout,
			TopP:             m.TopP,
		}
		return models.NewOpenAIModelWithLogger(cfg, log)

	case *config.AzureOpenAI:
		cfg := &models.AzureOpenAIConfig{
			Model:   m.Model,
			Headers: extractHeaders(m.Headers),
			Timeout: nil,
		}
		return models.NewAzureOpenAIModelWithLogger(cfg, log)

	case *config.Gemini:
		apiKey := os.Getenv("GOOGLE_API_KEY")
		if apiKey == "" {
			apiKey = os.Getenv("GEMINI_API_KEY")
		}
		if apiKey == "" {
			return nil, fmt.Errorf("gemini model requires GOOGLE_API_KEY or GEMINI_API_KEY environment variable")
		}
		modelName := m.Model
		if modelName == "" {
			modelName = "gemini-2.0-flash"
		}
		return adkgemini.NewModel(ctx, modelName, &genai.ClientConfig{APIKey: apiKey})

	case *config.GeminiVertexAI:
		project := os.Getenv("GOOGLE_CLOUD_PROJECT")
		location := os.Getenv("GOOGLE_CLOUD_LOCATION")
		if location == "" {
			location = os.Getenv("GOOGLE_CLOUD_REGION")
		}
		if project == "" || location == "" {
			return nil, fmt.Errorf("GeminiVertexAI requires GOOGLE_CLOUD_PROJECT and GOOGLE_CLOUD_LOCATION (or GOOGLE_CLOUD_REGION) environment variables")
		}
		modelName := m.Model
		if modelName == "" {
			modelName = "gemini-2.0-flash"
		}
		return adkgemini.NewModel(ctx, modelName, &genai.ClientConfig{
			Backend:  genai.BackendVertexAI,
			Project:  project,
			Location: location,
		})

	case *config.Anthropic:
		modelName := m.Model
		if modelName == "" {
			modelName = "claude-sonnet-4-20250514"
		}
		cfg := &models.AnthropicConfig{
			Model:       modelName,
			BaseUrl:     m.BaseUrl,
			Headers:     extractHeaders(m.Headers),
			MaxTokens:   m.MaxTokens,
			Temperature: m.Temperature,
			TopP:        m.TopP,
			TopK:        m.TopK,
			Timeout:     m.Timeout,
		}
		return models.NewAnthropicModelWithLogger(cfg, log)

	case *config.Ollama:
		baseURL := os.Getenv("OLLAMA_API_BASE")
		if baseURL == "" {
			baseURL = "http://localhost:11434"
		}
		baseURL = strings.TrimSuffix(baseURL, "/")
		if !strings.HasSuffix(baseURL, "/v1") {
			baseURL += "/v1"
		}
		modelName := m.Model
		if modelName == "" {
			modelName = "llama3.2"
		}
		return models.NewOpenAICompatibleModelWithLogger(baseURL, modelName, extractHeaders(m.Headers), "", log)

	case *config.Bedrock:
		region := m.Region
		if region == "" {
			region = os.Getenv("AWS_REGION")
		}
		if region == "" {
			return nil, fmt.Errorf("bedrock requires AWS_REGION environment variable or region in model config")
		}
		modelName := m.Model
		if modelName == "" {
			return nil, fmt.Errorf("bedrock requires a model name (e.g. anthropic.claude-3-sonnet-20240229-v1:0)")
		}
		cfg := &models.AnthropicConfig{
			Model:   modelName,
			Headers: extractHeaders(m.Headers),
		}
		return models.NewAnthropicBedrockModelWithLogger(ctx, cfg, region, log)

	case *config.GeminiAnthropic:
		// GeminiAnthropic = Claude models accessed through Google Cloud Vertex AI.
		// Uses the Anthropic SDK's built-in Vertex AI support with Application Default Credentials.
		project := os.Getenv("GOOGLE_CLOUD_PROJECT")
		region := os.Getenv("GOOGLE_CLOUD_LOCATION")
		if region == "" {
			region = os.Getenv("GOOGLE_CLOUD_REGION")
		}
		if project == "" || region == "" {
			return nil, fmt.Errorf("GeminiAnthropic (Anthropic on Vertex AI) requires GOOGLE_CLOUD_PROJECT and GOOGLE_CLOUD_LOCATION environment variables")
		}
		modelName := m.Model
		if modelName == "" {
			modelName = "claude-sonnet-4-20250514"
		}
		cfg := &models.AnthropicConfig{
			Model:   modelName,
			Headers: extractHeaders(m.Headers),
		}
		return models.NewAnthropicVertexAIModelWithLogger(ctx, cfg, region, project, log)

	default:
		return nil, fmt.Errorf("unsupported model type: %s", m.GetType())
	}
}

// extractHeaders returns an empty map if nil, the original map otherwise.
func extractHeaders(headers map[string]string) map[string]string {
	if headers == nil {
		return make(map[string]string)
	}
	return headers
}

// makeBeforeToolCallback returns a BeforeToolCallback that logs tool invocations.
func makeBeforeToolCallback(logger logr.Logger) llmagent.BeforeToolCallback {
	return func(ctx tool.Context, t tool.Tool, args map[string]any) (map[string]any, error) {
		logger.Info("Tool execution started",
			"tool", t.Name(),
			"functionCallID", ctx.FunctionCallID(),
			"sessionID", ctx.SessionID(),
			"invocationID", ctx.InvocationID(),
			"args", truncateArgs(args),
		)
		return nil, nil
	}
}

// makeAfterToolCallback returns an AfterToolCallback that logs tool completion.
func makeAfterToolCallback(logger logr.Logger) llmagent.AfterToolCallback {
	return func(ctx tool.Context, t tool.Tool, args, result map[string]any, err error) (map[string]any, error) {
		if err != nil {
			logger.Error(err, "Tool execution completed with error",
				"tool", t.Name(),
				"functionCallID", ctx.FunctionCallID(),
				"sessionID", ctx.SessionID(),
				"invocationID", ctx.InvocationID(),
			)
		} else {
			logger.Info("Tool execution completed",
				"tool", t.Name(),
				"functionCallID", ctx.FunctionCallID(),
				"sessionID", ctx.SessionID(),
				"invocationID", ctx.InvocationID(),
				"resultKeys", mapKeys(result),
			)
		}
		return nil, nil
	}
}

// makeOnToolErrorCallback returns an OnToolErrorCallback that logs tool errors.
func makeOnToolErrorCallback(logger logr.Logger) llmagent.OnToolErrorCallback {
	return func(ctx tool.Context, t tool.Tool, args map[string]any, err error) (map[string]any, error) {
		logger.Error(err, "Tool execution failed",
			"tool", t.Name(),
			"functionCallID", ctx.FunctionCallID(),
			"sessionID", ctx.SessionID(),
			"invocationID", ctx.InvocationID(),
			"args", truncateArgs(args),
		)
		return nil, nil
	}
}

// mapKeys returns the top-level keys of a map for logging without exposing values.
func mapKeys(m map[string]any) []string {
	if m == nil {
		return nil
	}
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	return keys
}

// truncateArgs returns a JSON string of args truncated for safe logging.
func truncateArgs(args map[string]any) string {
	const (
		maxValueLen = 100
		maxTotalLen = 500
	)
	if args == nil {
		return "{}"
	}
	truncated := make(map[string]any, len(args))
	for k, v := range args {
		if s, ok := v.(string); ok && len(s) > maxValueLen {
			truncated[k] = s[:maxValueLen] + "..."
		} else {
			truncated[k] = v
		}
	}
	b, err := json.Marshal(truncated)
	if err != nil {
		return fmt.Sprintf("<marshal error: %v>", err)
	}
	s := string(b)
	if len(s) > maxTotalLen {
		return s[:maxTotalLen] + "... (truncated)"
	}
	return s
}
