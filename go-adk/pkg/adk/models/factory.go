package models

import (
	"context"
	"fmt"
	"os"

	"github.com/go-logr/logr"
	"github.com/kagent-dev/kagent/go-adk/pkg/core/types"
	adkmodel "google.golang.org/adk/model"

	adkgemini "google.golang.org/adk/model/gemini"
	"google.golang.org/genai"
)

// LLMFactory creates adkmodel.LLM instances from types.Model configurations.
type LLMFactory interface {
	// CreateLLM creates a adkmodel.LLM from a types.Model configuration.
	CreateLLM(ctx context.Context, m types.Model, logger logr.Logger) (adkmodel.LLM, error)

	// SupportedTypes returns the model types this factory supports.
	SupportedTypes() []string
}

// DefaultLLMFactory is the default LLM factory implementation.
type DefaultLLMFactory struct {
	// providers maps model type to provider functions
	providers map[string]LLMProviderFunc
}

// LLMProviderFunc is a function that creates a adkmodel.LLM for a specific model type.
type LLMProviderFunc func(ctx context.Context, m types.Model, logger logr.Logger) (adkmodel.LLM, error)

// NewDefaultLLMFactory creates a new default LLM factory with all built-in providers.
func NewDefaultLLMFactory() *DefaultLLMFactory {
	f := &DefaultLLMFactory{
		providers: make(map[string]LLMProviderFunc),
	}

	// Register built-in providers
	f.RegisterProvider(types.ModelTypeOpenAI, createOpenAILLM)
	f.RegisterProvider(types.ModelTypeAzureOpenAI, createAzureOpenAILLM)
	f.RegisterProvider(types.ModelTypeAnthropic, createAnthropicLLM)
	f.RegisterProvider(types.ModelTypeGemini, createGeminiLLM)
	f.RegisterProvider(types.ModelTypeGeminiVertexAI, createGeminiVertexAILLM)
	f.RegisterProvider(types.ModelTypeOllama, createOllamaLLM)
	f.RegisterProvider(types.ModelTypeGeminiAnthropic, createGeminiAnthropicLLM)

	return f
}

// RegisterProvider registers a provider for a model type.
func (f *DefaultLLMFactory) RegisterProvider(modelType string, provider LLMProviderFunc) {
	f.providers[modelType] = provider
}

// CreateLLM implements LLMFactory.
func (f *DefaultLLMFactory) CreateLLM(ctx context.Context, m types.Model, logger logr.Logger) (adkmodel.LLM, error) {
	provider, ok := f.providers[m.GetType()]
	if !ok {
		return nil, fmt.Errorf("unsupported model type: %s", m.GetType())
	}
	return provider(ctx, m, logger)
}

// SupportedTypes implements LLMFactory.
func (f *DefaultLLMFactory) SupportedTypes() []string {
	types := make([]string, 0, len(f.providers))
	for t := range f.providers {
		types = append(types, t)
	}
	return types
}

// Provider implementations

func createOpenAILLM(ctx context.Context, m types.Model, logger logr.Logger) (adkmodel.LLM, error) {
	openai, ok := m.(*types.OpenAI)
	if !ok {
		return nil, fmt.Errorf("expected *types.OpenAI, got %T", m)
	}

	config := &OpenAIConfig{
		Model:            openai.Model,
		BaseUrl:          openai.BaseUrl,
		Headers:          extractHeaders(openai.Headers),
		FrequencyPenalty: openai.FrequencyPenalty,
		MaxTokens:        openai.MaxTokens,
		N:                openai.N,
		PresencePenalty:  openai.PresencePenalty,
		ReasoningEffort:  openai.ReasoningEffort,
		Seed:             openai.Seed,
		Temperature:      openai.Temperature,
		Timeout:          openai.Timeout,
		TopP:             openai.TopP,
	}
	return NewOpenAIModelWithLogger(config, logger)
}

func createAzureOpenAILLM(ctx context.Context, m types.Model, logger logr.Logger) (adkmodel.LLM, error) {
	azure, ok := m.(*types.AzureOpenAI)
	if !ok {
		return nil, fmt.Errorf("expected *types.AzureOpenAI, got %T", m)
	}

	config := &AzureOpenAIConfig{
		Model:   azure.Model,
		Headers: extractHeaders(azure.Headers),
		Timeout: nil,
	}
	return NewAzureOpenAIModelWithLogger(config, logger)
}

func createAnthropicLLM(ctx context.Context, m types.Model, logger logr.Logger) (adkmodel.LLM, error) {
	anthropic, ok := m.(*types.Anthropic)
	if !ok {
		return nil, fmt.Errorf("expected *types.Anthropic, got %T", m)
	}

	modelName := anthropic.Model
	if modelName == "" {
		modelName = "claude-sonnet-4-20250514"
	}

	config := &AnthropicConfig{
		Model:       modelName,
		BaseUrl:     anthropic.BaseUrl,
		Headers:     extractHeaders(anthropic.Headers),
		MaxTokens:   anthropic.MaxTokens,
		Temperature: anthropic.Temperature,
		TopP:        anthropic.TopP,
		TopK:        anthropic.TopK,
		Timeout:     anthropic.Timeout,
	}
	return NewAnthropicModelWithLogger(config, logger)
}

func createGeminiLLM(ctx context.Context, m types.Model, logger logr.Logger) (adkmodel.LLM, error) {
	gemini, ok := m.(*types.Gemini)
	if !ok {
		return nil, fmt.Errorf("expected *types.Gemini, got %T", m)
	}

	apiKey := os.Getenv("GOOGLE_API_KEY")
	if apiKey == "" {
		apiKey = os.Getenv("GEMINI_API_KEY")
	}
	if apiKey == "" {
		return nil, fmt.Errorf("Gemini model requires GOOGLE_API_KEY or GEMINI_API_KEY environment variable")
	}

	modelName := gemini.Model
	if modelName == "" {
		modelName = "gemini-2.0-flash"
	}

	return adkgemini.NewModel(ctx, modelName, &genai.ClientConfig{APIKey: apiKey})
}

func createGeminiVertexAILLM(ctx context.Context, m types.Model, logger logr.Logger) (adkmodel.LLM, error) {
	vertexAI, ok := m.(*types.GeminiVertexAI)
	if !ok {
		return nil, fmt.Errorf("expected *types.GeminiVertexAI, got %T", m)
	}

	project := os.Getenv("GOOGLE_CLOUD_PROJECT")
	location := os.Getenv("GOOGLE_CLOUD_LOCATION")
	if location == "" {
		location = os.Getenv("GOOGLE_CLOUD_REGION")
	}
	if project == "" || location == "" {
		return nil, fmt.Errorf("GeminiVertexAI requires GOOGLE_CLOUD_PROJECT and GOOGLE_CLOUD_LOCATION (or GOOGLE_CLOUD_REGION) environment variables")
	}

	modelName := vertexAI.Model
	if modelName == "" {
		modelName = "gemini-2.0-flash"
	}

	return adkgemini.NewModel(ctx, modelName, &genai.ClientConfig{
		Backend:  genai.BackendVertexAI,
		Project:  project,
		Location: location,
	})
}

func createOllamaLLM(ctx context.Context, m types.Model, logger logr.Logger) (adkmodel.LLM, error) {
	ollama, ok := m.(*types.Ollama)
	if !ok {
		return nil, fmt.Errorf("expected *types.Ollama, got %T", m)
	}

	baseURL := "http://localhost:11434/v1"
	modelName := ollama.Model
	if modelName == "" {
		modelName = "llama3.2"
	}

	return NewOpenAICompatibleModelWithLogger(baseURL, modelName, extractHeaders(ollama.Headers), "", logger)
}

func createGeminiAnthropicLLM(ctx context.Context, m types.Model, logger logr.Logger) (adkmodel.LLM, error) {
	geminiAnthropic, ok := m.(*types.GeminiAnthropic)
	if !ok {
		return nil, fmt.Errorf("expected *types.GeminiAnthropic, got %T", m)
	}

	baseURL := os.Getenv("LITELLM_BASE_URL")
	if baseURL == "" {
		return nil, fmt.Errorf("GeminiAnthropic (Claude) model requires LITELLM_BASE_URL or configure base_url (e.g. LiteLLM server URL)")
	}

	modelName := geminiAnthropic.Model
	if modelName == "" {
		modelName = "claude-3-5-sonnet-20241022"
	}
	liteLlmModel := "anthropic/" + modelName

	return NewOpenAICompatibleModelWithLogger(baseURL, liteLlmModel, extractHeaders(geminiAnthropic.Headers), "", logger)
}

// extractHeaders extracts headers from a map, returning an empty map if nil
func extractHeaders(headers map[string]string) map[string]string {
	if headers == nil {
		return make(map[string]string)
	}
	return headers
}
