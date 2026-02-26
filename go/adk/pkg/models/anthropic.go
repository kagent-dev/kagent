package models

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"time"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/bedrock"
	"github.com/anthropics/anthropic-sdk-go/option"
	"github.com/anthropics/anthropic-sdk-go/vertex"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/go-logr/logr"
)

// AnthropicConfig holds Anthropic configuration
type AnthropicConfig struct {
	Model       string
	BaseUrl     string            // Optional: override API base URL
	Headers     map[string]string // Default headers to pass to Anthropic API
	MaxTokens   *int
	Temperature *float64
	TopP        *float64
	TopK        *int
	Timeout     *int
}

// AnthropicModel implements model.LLM for Anthropic Claude models.
type AnthropicModel struct {
	Config *AnthropicConfig
	Client anthropic.Client
	Logger logr.Logger
}

// NewAnthropicModelWithLogger creates a new Anthropic model instance with a logger
func NewAnthropicModelWithLogger(config *AnthropicConfig, logger logr.Logger) (*AnthropicModel, error) {
	apiKey := os.Getenv("ANTHROPIC_API_KEY")
	if apiKey == "" {
		return nil, fmt.Errorf("ANTHROPIC_API_KEY environment variable is not set")
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

	// Set timeout
	timeout := defaultTimeout
	if config.Timeout != nil {
		timeout = time.Duration(*config.Timeout) * time.Second
	}
	httpClient := &http.Client{Timeout: timeout}

	// Add custom headers if provided
	if len(config.Headers) > 0 {
		httpClient.Transport = &headerTransport{
			base:    http.DefaultTransport,
			headers: config.Headers,
		}
		if logger.GetSink() != nil {
			logger.Info("Setting default headers for Anthropic client", "headersCount", len(config.Headers))
		}
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

// NewAnthropicVertexAIModelWithLogger creates an Anthropic model that authenticates
// via Google Cloud Vertex AI using Application Default Credentials (ADC).
// This is used for the GeminiAnthropic / AnthropicVertexAI provider type.
func NewAnthropicVertexAIModelWithLogger(ctx context.Context, config *AnthropicConfig, region, projectID string, logger logr.Logger) (*AnthropicModel, error) {
	opts := []option.RequestOption{
		vertex.WithGoogleAuth(ctx, region, projectID),
	}

	timeout := defaultTimeout
	if config.Timeout != nil {
		timeout = time.Duration(*config.Timeout) * time.Second
	}
	httpClient := &http.Client{Timeout: timeout}
	if len(config.Headers) > 0 {
		httpClient.Transport = &headerTransport{
			base:    http.DefaultTransport,
			headers: config.Headers,
		}
	}
	opts = append(opts, option.WithHTTPClient(httpClient))

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

	timeout := defaultTimeout
	if config.Timeout != nil {
		timeout = time.Duration(*config.Timeout) * time.Second
	}
	httpClient := &http.Client{Timeout: timeout}
	if len(config.Headers) > 0 {
		httpClient.Transport = &headerTransport{
			base:    http.DefaultTransport,
			headers: config.Headers,
		}
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
