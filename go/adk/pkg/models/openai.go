package models

import (
	"context"
	"fmt"
	"os"

	"github.com/go-logr/logr"
	"github.com/kagent-dev/kagent/go/adk/pkg/internal/azureai"
	"github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/option"
)

// OpenAIConfig holds OpenAI configuration
type OpenAIConfig struct {
	TransportConfig
	Model            string
	BaseUrl          string
	FrequencyPenalty *float64
	MaxTokens        *int
	N                *int
	PresencePenalty  *float64
	ReasoningEffort  *string
	Seed             *int
	Temperature      *float64
	TopP             *float64
}

// AzureOpenAIConfig holds Azure OpenAI configuration
type AzureOpenAIConfig struct {
	TransportConfig
	Model      string
	Endpoint   string
	Deployment string
	APIVersion string

	// credential overrides the Azure credential used for the implicit Workload
	// Identity auth path. When nil, azureai.NewDefaultCredential is used. It is
	// unexported and exists so tests can inject a fake credential.
	credential azureai.TokenCredential
}

// OpenAIModel implements model.LLM (see openai_adk.go) for OpenAI/Azure OpenAI.
type OpenAIModel struct {
	Config  *OpenAIConfig
	Client  openai.Client
	IsAzure bool
	Logger  logr.Logger
}

// NewOpenAIModelWithLogger creates a new OpenAI model instance with a logger
func NewOpenAIModelWithLogger(config *OpenAIConfig, logger logr.Logger) (*OpenAIModel, error) {
	apiKey := "passthrough" // placeholder; real auth set per-request by transport
	if !config.APIKeyPassthrough {
		apiKey = os.Getenv("OPENAI_API_KEY")
		if apiKey == "" {
			return nil, fmt.Errorf("OPENAI_API_KEY environment variable is not set")
		}
	}
	return newOpenAIModelFromConfig(config, apiKey, logger)
}

// NewOpenAICompatibleModelWithLogger creates an OpenAI-compatible model (e.g. LiteLLM, Ollama).
// baseURL is the API base (e.g. http://localhost:11434/v1 for Ollama). apiKey is optional; if empty,
// OPENAI_API_KEY is used, then a placeholder for endpoints that do not require a key.
func NewOpenAICompatibleModelWithLogger(baseURL, modelName string, headers map[string]string, apiKey string, logger logr.Logger) (*OpenAIModel, error) {
	if apiKey == "" {
		apiKey = os.Getenv("OPENAI_API_KEY")
	}
	if apiKey == "" {
		apiKey = "ollama" // placeholder for Ollama and similar endpoints that ignore key
	}
	config := &OpenAIConfig{
		TransportConfig: TransportConfig{Headers: headers},
		Model:           modelName,
		BaseUrl:         baseURL,
	}
	return newOpenAIModelFromConfig(config, apiKey, logger)
}

// TODO: consider support for Azure OpenAI, when used from NewOpenAICompatibleModelWithLogger,
// Anthropic and Gemini might use Azure OpenAI, so we need to support it.
func newOpenAIModelFromConfig(config *OpenAIConfig, apiKey string, logger logr.Logger) (*OpenAIModel, error) {
	opts := []option.RequestOption{
		option.WithAPIKey(apiKey),
	}
	if config.BaseUrl != "" {
		opts = append(opts, option.WithBaseURL(config.BaseUrl))
	}
	httpClient, err := BuildHTTPClient(config.TransportConfig)
	if err != nil {
		return nil, err
	}
	if logger.GetSink() != nil && len(config.Headers) > 0 {
		logger.Info("Setting default headers for OpenAI client", "headersCount", len(config.Headers), "headers", config.Headers)
	}
	opts = append(opts, option.WithHTTPClient(httpClient))

	client := openai.NewClient(opts...)
	if logger.GetSink() != nil {
		logger.Info("Initialized OpenAI model", "model", config.Model, "baseUrl", config.BaseUrl)
	}
	return &OpenAIModel{
		Config:  config,
		Client:  client,
		IsAzure: false,
		Logger:  logger,
	}, nil
}

// NewAzureOpenAIModelWithLogger creates a new Azure OpenAI model instance with a logger.
// It targets the Azure OpenAI OpenAI-compatible data plane
// (POST {endpoint}/openai/deployments/{deployment}/chat/completions) through the
// shared azureai client. Endpoint, api-version, and deployment come from the
// model config, with AZURE_OPENAI_ENDPOINT / OPENAI_API_VERSION env fallbacks.
//
// Authentication is implicit and mirrors Foundry: the incoming bearer token when
// APIKeyPassthrough is enabled; otherwise the AZURE_OPENAI_API_KEY Api-Key header
// when set; otherwise DefaultAzureCredential, which resolves to Azure Workload
// Identity in-cluster (or the az CLI in local development). The Workload Identity
// path eagerly acquires a token so a missing or misconfigured identity fails
// readiness at startup instead of on the first inference request.
func NewAzureOpenAIModelWithLogger(ctx context.Context, config *AzureOpenAIConfig, logger logr.Logger) (*OpenAIModel, error) {
	endpoint := config.Endpoint
	if endpoint == "" {
		endpoint = os.Getenv("AZURE_OPENAI_ENDPOINT")
	}
	if endpoint == "" {
		return nil, fmt.Errorf("AZURE_OPENAI_ENDPOINT environment variable is not set")
	}

	apiVersion := config.APIVersion
	if apiVersion == "" {
		apiVersion = os.Getenv("OPENAI_API_VERSION")
	}
	if apiVersion == "" {
		apiVersion = "2024-02-15-preview"
	}

	deployment := config.Deployment
	if deployment == "" {
		deployment = config.Model
	}

	httpClient, err := BuildHTTPClient(config.TransportConfig)
	if err != nil {
		return nil, err
	}

	clientCfg := azureai.ClientConfig{
		Endpoint:   endpoint,
		Deployment: deployment,
		APIVersion: apiVersion,
		HTTPClient: httpClient,
	}

	// Implicit auth: the incoming bearer token when APIKeyPassthrough is enabled
	// (a placeholder Api-Key is overwritten per request by openAIPassthroughOpts),
	// otherwise the AZURE_OPENAI_API_KEY Api-Key header, otherwise
	// DefaultAzureCredential (Workload Identity), eagerly probed for readiness.
	apiKey := os.Getenv("AZURE_OPENAI_API_KEY")
	if config.APIKeyPassthrough {
		apiKey = "passthrough"
	}
	if err := azureai.ApplyImplicitAuth(ctx, &clientCfg, azureai.AuthOptions{
		APIKey:     apiKey,
		Credential: config.credential,
		EagerProbe: true,
	}); err != nil {
		return nil, err
	}

	client, err := azureai.NewOpenAIClient(clientCfg)
	if err != nil {
		return nil, err
	}
	if logger.GetSink() != nil {
		logger.Info("Initialized Azure OpenAI model", "model", config.Model, "deployment", deployment, "endpoint", endpoint, "apiVersion", apiVersion)
	}
	return &OpenAIModel{
		Config: &OpenAIConfig{
			TransportConfig: config.TransportConfig,
			Model:           deployment,
		},
		Client:  client,
		IsAzure: true,
		Logger:  logger,
	}, nil
}

// openAIPassthroughOpts returns a per-request option that injects the bearer token from ctx
// For OpenAI the SDK sends this as "Authorization: Bearer <token>".
// For Azure the SDK sends this as "Api-Key: <token>" via option.WithHeader.
func openAIPassthroughOpts(ctx context.Context, m *OpenAIModel) []option.RequestOption {
	if m.Config == nil || !m.Config.APIKeyPassthrough {
		return nil
	}
	if token, ok := ctx.Value(BearerTokenKey).(string); ok && token != "" {
		if m.IsAzure {
			return []option.RequestOption{option.WithHeader("Api-Key", token)}
		}
		return []option.RequestOption{option.WithAPIKey(token)}
	}
	return nil
}
