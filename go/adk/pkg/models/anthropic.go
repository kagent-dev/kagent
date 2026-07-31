package models

import (
	"context"
	"fmt"
	"net/http"
	"os"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/bedrock"
	"github.com/anthropics/anthropic-sdk-go/option"
	"github.com/anthropics/anthropic-sdk-go/vertex"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/go-logr/logr"
	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"
)

// vertexAIScope is the OAuth2 scope required for Vertex AI. This mirrors the
// scope used internally by anthropic-sdk-go's vertex.WithGoogleAuth when no
// scopes are supplied, so behavior stays consistent when we resolve
// credentials ourselves via composeVertexHTTPClient.
const vertexAIScope = "https://www.googleapis.com/auth/cloud-platform"

// anthropicPassthroughOpts returns a per-request option that sets the Anthropic API key
// from the bearer token in ctx when APIKeyPassthrough is enabled. The Anthropic SDK sends
// this as the x-api-key header, which is the correct auth mechanism for Anthropic.
func anthropicPassthroughOpts(ctx context.Context, cfg *AnthropicConfig) []option.RequestOption {
	if !cfg.APIKeyPassthrough {
		return nil
	}
	if token, ok := ctx.Value(BearerTokenKey).(string); ok && token != "" {
		return []option.RequestOption{option.WithAPIKey(token)}
	}
	return nil
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
}

// AnthropicModel implements model.LLM for Anthropic Claude models.
type AnthropicModel struct {
	Config *AnthropicConfig
	Client anthropic.Client
	Logger logr.Logger
}

// NewAnthropicModelWithLogger creates a new Anthropic model instance with a logger
func NewAnthropicModelWithLogger(config *AnthropicConfig, logger logr.Logger) (*AnthropicModel, error) {
	apiKey := "passthrough" // placeholder; real auth set per-request by transport
	if !config.APIKeyPassthrough {
		apiKey = os.Getenv("ANTHROPIC_API_KEY")
		if apiKey == "" {
			return nil, fmt.Errorf("ANTHROPIC_API_KEY environment variable is not set")
		}
	}
	return newAnthropicModelFromConfig(config, apiKey, logger)
}

func newAnthropicModelFromConfig(config *AnthropicConfig, apiKey string, logger logr.Logger) (*AnthropicModel, error) {
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
	if len(config.Headers) > 0 && logger.GetSink() != nil {
		logger.Info("Setting default headers for Anthropic client", "headersCount", len(config.Headers))
	}
	opts = append(opts, option.WithHTTPClient(httpClient))

	client := anthropic.NewClient(opts...)
	if logger.GetSink() != nil {
		logger.Info("Initialized Anthropic model", "model", config.Model, "baseUrl", config.BaseUrl)
	}

	return &AnthropicModel{
		Config: config,
		Client: client,
		Logger: logger,
	}, nil
}

// composeVertexHTTPClient returns an *http.Client that layers OAuth2 token
// injection on top of the caller-supplied base transport (custom TLS, custom
// headers, connect timeout, etc.). It preserves base.Timeout so the request
// timeout from TransportConfig is honored.
//
// The composed client is what makes it safe to hand a fully-customized
// *http.Client to the Anthropic SDK for Vertex: without this, either the
// Vertex option would wipe our custom transport stack, or a naive
// WithHTTPClient would wipe the SDK's OAuth2-wrapped client.
func composeVertexHTTPClient(base *http.Client, tokenSource oauth2.TokenSource) *http.Client {
	baseTransport := base.Transport
	if baseTransport == nil {
		baseTransport = http.DefaultTransport
	}
	return &http.Client{
		Timeout: base.Timeout,
		Transport: &oauth2.Transport{
			Base:   baseTransport,
			Source: tokenSource,
		},
	}
}

// NewAnthropicVertexAIModelWithLogger creates an Anthropic model that authenticates
// via Google Cloud Vertex AI using Application Default Credentials (ADC).
// This is used for the GeminiAnthropic / AnthropicVertexAI provider type.
//
// Composition contract:
//   - vertex.WithCredentials is applied first so its base URL and its
//     URL-rewrite/anthropic_version middleware are registered.
//   - option.WithHTTPClient is applied last with a client we build ourselves
//     from the user's TransportConfig, wrapped by an oauth2.Transport so the
//     custom TLS / headers / timeout stack AND the Google OAuth2 token are
//     both applied on the wire. Base URL and middleware are stored on
//     separate fields on the SDK's request config and middleware is additive,
//     so only the HTTP client field is overridden.
func NewAnthropicVertexAIModelWithLogger(ctx context.Context, config *AnthropicConfig, region, projectID string, logger logr.Logger) (*AnthropicModel, error) {
	// Build the caller's HTTP client (TLS, headers, timeout).
	httpClient, err := BuildHTTPClient(config.TransportConfig)
	if err != nil {
		return nil, err
	}

	// Resolve Google Application Default Credentials ourselves so we can
	// wrap the token source into our transport stack. This mirrors what
	// vertex.WithGoogleAuth does internally, but avoids its panic-on-error
	// behavior and avoids the double credential lookup that would happen
	// if we let WithGoogleAuth build its own HTTP client only to discard it.
	creds, err := google.FindDefaultCredentials(ctx, vertexAIScope)
	if err != nil {
		return nil, fmt.Errorf("failed to find Google default credentials for Vertex AI: %w", err)
	}

	composedClient := composeVertexHTTPClient(httpClient, creds.TokenSource)

	opts := []option.RequestOption{
		// Registers Vertex base URL + URL-rewrite middleware.
		vertex.WithCredentials(ctx, region, projectID, creds),
		// Overrides only the HTTP client field. Base URL and middleware
		// registered above survive.
		option.WithHTTPClient(composedClient),
	}

	client := anthropic.NewClient(opts...)
	logger.Info("Initialized Anthropic Vertex AI model", "model", config.Model, "region", region, "project", projectID)

	return &AnthropicModel{
		Config: config,
		Client: client,
		Logger: logger,
	}, nil
}

// NewAnthropicBedrockModelWithLogger creates an Anthropic model that uses
// AWS Bedrock as the backend. Authentication is handled by the AWS SDK:
//   - If AWS_BEARER_TOKEN_BEDROCK is set, bearer token auth is used.
//   - Otherwise, standard AWS credentials (AWS_ACCESS_KEY_ID, AWS_SECRET_ACCESS_KEY,
//     AWS_SESSION_TOKEN) or IAM roles are used via SigV4 signing.
//
// The region must be provided (e.g. "us-east-1") and determines the Bedrock endpoint.
func NewAnthropicBedrockModelWithLogger(ctx context.Context, config *AnthropicConfig, region string, logger logr.Logger) (*AnthropicModel, error) {
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
	logger.Info("Initialized Anthropic Bedrock model", "model", config.Model, "region", region)

	return &AnthropicModel{
		Config: config,
		Client: client,
		Logger: logger,
	}, nil
}
