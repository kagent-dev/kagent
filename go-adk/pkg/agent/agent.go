package agent

import (
	"context"
	"fmt"
	"os"

	"github.com/go-logr/logr"
	"github.com/kagent-dev/kagent/go-adk/pkg/config"
	"github.com/kagent-dev/kagent/go-adk/pkg/model"
	"google.golang.org/adk/agent"
	"google.golang.org/adk/agent/llmagent"
	adkmodel "google.golang.org/adk/model"
	adkgemini "google.golang.org/adk/model/gemini"
	"google.golang.org/adk/tool"
	"google.golang.org/genai"
)

// CreateGoogleADKAgent creates a Google ADK agent from AgentConfig
func CreateGoogleADKAgent(ctx context.Context, agentConfig *config.AgentConfig, toolsets []tool.Toolset) (agent.Agent, error) {
	log := logr.FromContextOrDiscard(ctx)

	if agentConfig == nil {
		return nil, fmt.Errorf("agent config is required")
	}

	if agentConfig.Model == nil {
		return nil, fmt.Errorf("model configuration is required")
	}

	log.Info("MCP toolsets created", "totalToolsets", len(toolsets), "httpToolsCount", len(agentConfig.HttpTools), "sseToolsCount", len(agentConfig.SseTools))

	// Create the LLM model
	var llmModel adkmodel.LLM
	var err error

	switch m := agentConfig.Model.(type) {
	case *config.OpenAI:
		headers := extractHeaders(m.Headers)
		modelConfig := &model.OpenAIConfig{
			Model:            m.Model,
			BaseUrl:          m.BaseUrl,
			Headers:          headers,
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
		llmModel, err = model.NewOpenAIModel(ctx, modelConfig)
		if err != nil {
			return nil, fmt.Errorf("failed to create OpenAI model: %w", err)
		}
	case *config.AzureOpenAI:
		headers := extractHeaders(m.Headers)
		modelConfig := &model.AzureOpenAIConfig{
			Model:   m.Model,
			Headers: headers,
			Timeout: nil,
		}
		llmModel, err = model.NewAzureOpenAIModel(ctx, modelConfig)
		if err != nil {
			return nil, fmt.Errorf("failed to create Azure OpenAI model: %w", err)
		}

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
		llmModel, err = adkgemini.NewModel(ctx, modelName, &genai.ClientConfig{APIKey: apiKey})
		if err != nil {
			return nil, fmt.Errorf("failed to create Gemini model: %w", err)
		}
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
		llmModel, err = adkgemini.NewModel(ctx, modelName, &genai.ClientConfig{
			Backend:  genai.BackendVertexAI,
			Project:  project,
			Location: location,
		})
		if err != nil {
			return nil, fmt.Errorf("failed to create Gemini Vertex AI model: %w", err)
		}

	case *config.Anthropic:
		modelName := m.Model
		if modelName == "" {
			modelName = "claude-sonnet-4-20250514"
		}
		modelConfig := &model.AnthropicConfig{
			Model:       modelName,
			BaseUrl:     m.BaseUrl,
			Headers:     extractHeaders(m.Headers),
			MaxTokens:   m.MaxTokens,
			Temperature: m.Temperature,
			TopP:        m.TopP,
			TopK:        m.TopK,
			Timeout:     m.Timeout,
		}
		llmModel, err = model.NewAnthropicModel(ctx, modelConfig)
		if err != nil {
			return nil, fmt.Errorf("failed to create Anthropic model: %w", err)
		}
	case *config.Ollama:
		baseURL := "http://localhost:11434/v1"
		modelName := m.Model
		if modelName == "" {
			modelName = "llama3.2"
		}
		llmModel, err = model.NewOpenAICompatibleModel(ctx, baseURL, modelName, extractHeaders(m.Headers), "")
		if err != nil {
			return nil, fmt.Errorf("failed to create Ollama model: %w", err)
		}
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
		llmModel, err = model.NewOpenAICompatibleModel(ctx, baseURL, liteLlmModel, extractHeaders(m.Headers), "")
		if err != nil {
			return nil, fmt.Errorf("failed to create GeminiAnthropic (Claude) model: %w", err)
		}

	default:
		return nil, fmt.Errorf("unsupported model type: %s", agentConfig.Model.GetType())
	}

	agentName := "agent"
	if agentConfig.Description != "" {
		agentName = "agent"
	}

	llmAgentConfig := llmagent.Config{
		Name:            agentName,
		Description:     agentConfig.Description,
		Instruction:     agentConfig.Instruction,
		Model:           llmModel,
		IncludeContents: llmagent.IncludeContentsDefault,
		Toolsets:        toolsets,
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

// extractHeaders extracts headers from a map, returning an empty map if nil
func extractHeaders(headers map[string]string) map[string]string {
	if headers == nil {
		return make(map[string]string)
	}
	return headers
}
