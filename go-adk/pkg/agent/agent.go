package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"os"

	"github.com/go-logr/logr"
	"github.com/kagent-dev/kagent/go-adk/pkg/config"
	"github.com/kagent-dev/kagent/go-adk/pkg/models"
	"google.golang.org/adk/agent"
	"google.golang.org/adk/agent/llmagent"
	adkmodel "google.golang.org/adk/model"
	adkgemini "google.golang.org/adk/model/gemini"
	"google.golang.org/adk/tool"
	"google.golang.org/genai"
)

// CreateGoogleADKAgent creates a Google ADK agent from AgentConfig.
// Toolsets are passed in directly (created by mcp.CreateToolsets).
func CreateGoogleADKAgent(ctx context.Context, agentConfig *config.AgentConfig, toolsets []tool.Toolset) (agent.Agent, error) {
	log := logr.FromContextOrDiscard(ctx)

	if agentConfig == nil {
		return nil, fmt.Errorf("agent config is required")
	}

	if agentConfig.Model == nil {
		return nil, fmt.Errorf("model configuration is required")
	}

	log.Info("MCP toolsets created", "totalToolsets", len(toolsets), "httpToolsCount", len(agentConfig.HttpTools), "sseToolsCount", len(agentConfig.SseTools))

	llmModel, err := createLLM(ctx, agentConfig.Model, log)
	if err != nil {
		return nil, fmt.Errorf("failed to create LLM: %w", err)
	}

	llmAgentConfig := llmagent.Config{
		Name:            "agent",
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
			return nil, fmt.Errorf("Gemini model requires GOOGLE_API_KEY or GEMINI_API_KEY environment variable")
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
		baseURL := "http://localhost:11434/v1"
		modelName := m.Model
		if modelName == "" {
			modelName = "llama3.2"
		}
		return models.NewOpenAICompatibleModelWithLogger(baseURL, modelName, extractHeaders(m.Headers), "", log)

	case *config.GeminiAnthropic:
		baseURL := os.Getenv("LITELLM_BASE_URL")
		if baseURL == "" {
			return nil, fmt.Errorf("GeminiAnthropic (Claude) model requires LITELLM_BASE_URL or configure base_url (e.g. LiteLLM server URL)")
		}
		modelName := m.Model
		if modelName == "" {
			modelName = "claude-3-5-sonnet-20241022"
		}
		liteLlmModel := "anthropic/" + modelName
		return models.NewOpenAICompatibleModelWithLogger(baseURL, liteLlmModel, extractHeaders(m.Headers), "", log)

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
	const maxLen = 1000
	if args == nil {
		return "{}"
	}
	b, err := json.Marshal(args)
	if err != nil {
		return fmt.Sprintf("<marshal error: %v>", err)
	}
	s := string(b)
	if len(s) > maxLen {
		return s[:maxLen] + "... (truncated)"
	}
	return s
}
