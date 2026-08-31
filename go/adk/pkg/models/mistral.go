package models

import (
	"fmt"
	"os"

	"github.com/go-logr/logr"
	"github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/option"
)

// DefaultMistralBaseURL is the default endpoint for the Mistral AI cloud API.
// The MISTRAL_API_BASE environment variable and MistralConfig.BaseUrl both
// override it (self-hosted or Le Platforme regional endpoints).
const DefaultMistralBaseURL = "https://api.mistral.ai/v1"

// MistralConfig holds Mistral AI configuration. Mistral speaks the OpenAI
// wire protocol, so the runtime reuses the OpenAI SDK client and honors the
// same generation parameters (temperature, top_p, max_tokens, timeout).
type MistralConfig struct {
	TransportConfig
	Model       string
	BaseUrl     string
	MaxTokens   *int
	Temperature *float64
	TopP        *float64
	Timeout     *int
}

// MistralModel implements model.LLM (see mistral_adk.go) for Mistral AI.
// It wraps an OpenAIModel because Mistral exposes an OpenAI-compatible API
// (POST {base_url}/chat/completions with a Bearer token).
type MistralModel struct {
	Config *MistralConfig
	inner  *OpenAIModel
	Logger logr.Logger
}

// NewMistralModelWithLogger creates a new Mistral model instance with a logger.
// It reads MISTRAL_API_KEY unless APIKeyPassthrough is enabled; base URL falls
// back to MISTRAL_API_BASE then DefaultMistralBaseURL.
func NewMistralModelWithLogger(config *MistralConfig, logger logr.Logger) (*MistralModel, error) {
	apiKey := "passthrough" // placeholder; real auth set per-request by transport
	if !config.APIKeyPassthrough {
		apiKey = os.Getenv("MISTRAL_API_KEY")
		if apiKey == "" {
			return nil, fmt.Errorf("MISTRAL_API_KEY environment variable is not set")
		}
	}

	baseURL := config.BaseUrl
	if baseURL == "" {
		baseURL = os.Getenv("MISTRAL_API_BASE")
	}
	if baseURL == "" {
		baseURL = DefaultMistralBaseURL
	}

	opts := []option.RequestOption{
		option.WithAPIKey(apiKey),
		option.WithBaseURL(baseURL),
	}
	httpClient, err := BuildHTTPClient(config.TransportConfig)
	if err != nil {
		return nil, err
	}
	if logger.GetSink() != nil && len(config.Headers) > 0 {
		logger.Info("Setting default headers for Mistral client", "headersCount", len(config.Headers))
	}
	opts = append(opts, option.WithHTTPClient(httpClient))

	client := openai.NewClient(opts...)
	if logger.GetSink() != nil {
		logger.Info("Initialized Mistral model", "model", config.Model, "baseUrl", baseURL)
	}

	inner := &OpenAIModel{
		Config: &OpenAIConfig{
			TransportConfig: config.TransportConfig,
			Model:           config.Model,
			BaseUrl:         baseURL,
			MaxTokens:       config.MaxTokens,
			Temperature:     config.Temperature,
			TopP:            config.TopP,
		},
		Client:  client,
		IsAzure: false,
		Logger:  logger,
	}
	return &MistralModel{
		Config: config,
		inner:  inner,
		Logger: logger,
	}, nil
}
