package models

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/option"

	"github.com/kagent-dev/kagent/go/core/pkg/env"
	"github.com/kagent-dev/kagent/go/pkg/logging"
)

// defaultXAIBaseURL is the default endpoint for the xAI API. The XAI_API_BASE
// environment variable and XAIConfig.BaseUrl both override it.
const defaultXAIBaseURL = "https://api.x.ai/v1"

// XAIConfig holds xAI configuration. xAI speaks the OpenAI wire protocol, so
// the runtime reuses the OpenAI SDK client and honors the same generation
// parameters (temperature, top_p, token limit, timeout).
type XAIConfig struct {
	TransportConfig
	Model       string
	BaseUrl     string
	MaxTokens   *int64
	Temperature *float64
	TopP        *float64
}

// XAIModel implements model.LLM (see xai_adk.go) for xAI Grok models.
// It wraps an OpenAIModel because xAI exposes an OpenAI-compatible API
// (POST {base_url}/chat/completions with a Bearer token).
type XAIModel struct {
	Config *XAIConfig
	inner  *OpenAIModel
	Logger *slog.Logger
}

// NewXAIModel creates a new xAI model instance.
// It reads XAI_API_KEY unless APIKeyPassthrough is enabled; base URL falls
// back to XAI_API_BASE then defaultXAIBaseURL.
func NewXAIModel(ctx context.Context, config *XAIConfig) (*XAIModel, error) {
	logger := logging.FromContext(ctx)
	apiKey := "passthrough" // placeholder; real auth set per-request by transport
	if !config.APIKeyPassthrough {
		apiKey = env.XAIAPIKey.Get()
		if apiKey == "" {
			return nil, fmt.Errorf("XAI_API_KEY environment variable is not set")
		}
	}

	baseURL := config.BaseUrl
	if baseURL == "" {
		baseURL = env.XAIAPIBase.Get()
	}
	if baseURL == "" {
		baseURL = defaultXAIBaseURL
	}

	httpClient, err := BuildHTTPClient(config.TransportConfig)
	if err != nil {
		return nil, err
	}
	if len(config.Headers) > 0 {
		logger.InfoContext(ctx, "setting default headers for xAI client", "headers_count", len(config.Headers))
	}
	client := openai.NewClient(
		option.WithAPIKey(apiKey),
		option.WithBaseURL(baseURL),
		option.WithHTTPClient(httpClient),
	)
	logger.InfoContext(ctx, "initialized xAI model", "model", config.Model, "base_url", baseURL)

	inner := &OpenAIModel{
		Config: &OpenAIConfig{
			TransportConfig: config.TransportConfig,
			Model:           config.Model,
			BaseUrl:         baseURL,
			// xAI deprecates max_tokens on chat completions, so the configured
			// limit is sent as max_completion_tokens.
			MaxCompletionTokens: config.MaxTokens,
			Temperature:         config.Temperature,
			TopP:                config.TopP,
		},
		Client:  client,
		IsAzure: false,
		Logger:  logger,
	}
	return &XAIModel{
		Config: config,
		inner:  inner,
		Logger: logger,
	}, nil
}
