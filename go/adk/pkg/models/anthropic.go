package models

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"
	"github.com/anthropics/anthropic-sdk-go/vertex"

	"github.com/kagent-dev/kagent/go/core/pkg/env"
	"github.com/kagent-dev/kagent/go/pkg/logging"
)

// anthropicPassthroughOpts returns a per-request option that sets the Anthropic API key
// from the bearer token in ctx when APIKeyPassthrough is enabled. The Anthropic SDK sends
// this as the x-api-key header, which is the correct auth mechanism for Anthropic.
func anthropicPassthroughOpts(ctx context.Context, cfg *AnthropicConfig) []option.RequestOption {
	token, ok := PassthroughToken(ctx, cfg.APIKeyPassthrough)
	if !ok {
		return nil
	}
	return []option.RequestOption{option.WithAPIKey(token)}
}

// AnthropicConfig holds Anthropic configuration
type AnthropicConfig struct {
	TransportConfig
	Model       string
	BaseUrl     string // Optional: override API base URL
	MaxTokens   *int64
	Temperature *float64
	TopP        *float64
	TopK        *int64
	// PromptCaching, when true, marks the last tool definition, the last system
	// prompt block and the last block of the latest conversation turn with a
	// cache_control breakpoint so Anthropic bills the stable prefix of an agent
	// loop as a cache read instead of fresh input. See markAnthropicCacheBreakpoints.
	PromptCaching bool
	// CacheTTL selects the cache retention window when PromptCaching is on.
	// "" or "5m" uses the API's default 5-minute cache; "1h" opts into the
	// 1-hour cache, which is billed at a higher cache-write rate. See anthropicCacheControl.
	CacheTTL string
}

// AnthropicModel implements model.LLM for Anthropic Claude models.
type AnthropicModel struct {
	Config *AnthropicConfig
	Client anthropic.Client
	Logger *slog.Logger
}

// NewAnthropicModel creates a new Anthropic model instance with a logger
func NewAnthropicModel(ctx context.Context, config *AnthropicConfig) (*AnthropicModel, error) {
	apiKey := "passthrough" // placeholder; real auth set per-request by transport
	if !config.APIKeyPassthrough {
		apiKey = env.AnthropicAPIKey.Get()
		if apiKey == "" {
			return nil, fmt.Errorf("ANTHROPIC_API_KEY environment variable is not set")
		}
	}
	return newAnthropicModelFromConfig(ctx, config, apiKey)
}

func newAnthropicModelFromConfig(ctx context.Context, config *AnthropicConfig, apiKey string) (*AnthropicModel, error) {
	logger := logging.FromContext(ctx)
	opts := []option.RequestOption{
		option.WithAPIKey(apiKey),
	}

	// Set base URL if provided (useful for proxies or custom endpoints)
	if config.BaseUrl != "" {
		opts = append(opts, option.WithBaseURL(config.BaseUrl))
	}

	// Create HTTP client with TLS, custom headers, and timeout.
	httpClient, err := BuildHTTPClient(config.TransportConfig)
	if err != nil {
		return nil, err
	}
	if len(config.Headers) > 0 {
		logger.InfoContext(ctx, "setting default headers for Anthropic client", "headers_count", len(config.Headers))
	}
	opts = append(opts, option.WithHTTPClient(httpClient))

	client := anthropic.NewClient(opts...)
	logger.InfoContext(ctx, "initialized Anthropic model", "model", config.Model, "base_url", config.BaseUrl)

	return &AnthropicModel{
		Config: config,
		Client: client,
		Logger: logger,
	}, nil
}

// NewAnthropicVertexAIModel creates an Anthropic model that authenticates
// via Google Cloud Vertex AI using Application Default Credentials (ADC).
// This is used for the GeminiAnthropic / AnthropicVertexAI provider type.
func NewAnthropicVertexAIModel(ctx context.Context, config *AnthropicConfig, region, projectID string) (*AnthropicModel, error) {
	logger := logging.FromContext(ctx)
	opts := []option.RequestOption{
		vertex.WithGoogleAuth(ctx, region, projectID),
	}

	// Create HTTP client with timeout, custom headers, TLS, and passthrough
	httpClient, err := BuildHTTPClient(config.TransportConfig)
	if err != nil {
		return nil, err
	}
	opts = append(opts, option.WithHTTPClient(httpClient))

	client := anthropic.NewClient(opts...)
	logger.InfoContext(ctx, "initialized Anthropic Vertex AI model", "model", config.Model, "region", region, "project", projectID)

	return &AnthropicModel{
		Config: config,
		Client: client,
		Logger: logger,
	}, nil
}
