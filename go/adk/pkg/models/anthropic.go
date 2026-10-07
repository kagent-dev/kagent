package models

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/bedrock"
	"github.com/anthropics/anthropic-sdk-go/option"
	"github.com/anthropics/anthropic-sdk-go/vertex"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/kagent-dev/kagent/go/core/pkg/env"
	"github.com/kagent-dev/kagent/go/pkg/logging"
	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"
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
	MaxTokens   *int
	Temperature *float64
	TopP        *float64
	TopK        *int
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

// NewAnthropicVertexAIModel creates an Anthropic model for Claude on Google
// Cloud Vertex AI, the GeminiAnthropic / AnthropicVertexAI provider type. It
// authenticates with Application Default Credentials (ADC), unless
// KAGENT_SKIP_VERTEX_AUTH says the egress gateway sets the access token on
// every call: then the client never looks for ADC and sends an inert
// placeholder the gateway overwrites, as every other provider's placeholder
// API key is.
func NewAnthropicVertexAIModel(ctx context.Context, config *AnthropicConfig, region, projectID string) (*AnthropicModel, error) {
	logger := logging.FromContext(ctx)
	// Create HTTP client with timeout, custom headers, TLS, and passthrough
	httpClient, err := BuildHTTPClient(config.TransportConfig)
	if err != nil {
		return nil, err
	}
	// The Vertex option wraps the HTTP client passed before it with its
	// authorization; passed after, the client would replace the authorized one.
	opts := []option.RequestOption{option.WithHTTPClient(httpClient)}
	if env.SkipVertexAuth.Get() {
		opts = append(opts, vertex.WithCredentials(ctx, region, projectID, gatewayCredentials()))
	} else {
		opts = append(opts, vertex.WithGoogleAuth(ctx, region, projectID))
	}

	client := anthropic.NewClient(opts...)
	logger.InfoContext(ctx, "initialized Anthropic Vertex AI model", "model", config.Model, "region", region, "project", projectID)

	return &AnthropicModel{
		Config: config,
		Client: client,
		Logger: logger,
	}, nil
}

// gatewayCredentials stand in for Google credentials when the egress gateway
// authenticates the Vertex AI call: the SDK's Vertex adaptation needs a token
// source to build its client, and the placeholder it sends is overwritten by
// the access token the gateway minted from the service account key.
func gatewayCredentials() *google.Credentials {
	return &google.Credentials{TokenSource: oauth2.StaticTokenSource(&oauth2.Token{AccessToken: "kagent-credential-injected"})}
}

// NewAnthropicBedrockModel creates an Anthropic model that uses
// AWS Bedrock as the backend. Authentication is handled by the AWS SDK:
//   - If AWS_BEARER_TOKEN_BEDROCK is set, bearer token auth is used.
//   - Otherwise, standard AWS credentials (AWS_ACCESS_KEY_ID, AWS_SECRET_ACCESS_KEY,
//     AWS_SESSION_TOKEN) or IAM roles are used via SigV4 signing.
//
// The region must be provided (e.g. "us-east-1") and determines the Bedrock endpoint.
func NewAnthropicBedrockModel(ctx context.Context, config *AnthropicConfig, region string) (*AnthropicModel, error) {
	logger := logging.FromContext(ctx)
	opts := []option.RequestOption{
		bedrock.WithLoadDefaultConfig(ctx,
			awsconfig.WithRegion(region),
		),
	}

	// Create HTTP client with timeout, custom headers, TLS, and passthrough
	httpClient, err := BuildHTTPClient(config.TransportConfig)
	if err != nil {
		return nil, err
	}
	opts = append(opts, option.WithHTTPClient(httpClient))

	client := anthropic.NewClient(opts...)
	logger.InfoContext(ctx, "initialized Anthropic Bedrock model", "model", config.Model, "region", region)

	return &AnthropicModel{
		Config: config,
		Client: client,
		Logger: logger,
	}, nil
}
