package models

import (
	"fmt"
	"net/http"
	"os"
	"time"

	"github.com/go-logr/logr"
	"github.com/sashabaranov/go-openai"
)

// OpenAIConfig holds OpenAI configuration
type OpenAIConfig struct {
	Model            string
	BaseUrl          string
	Headers          map[string]string // Default headers to pass to OpenAI API (matching Python default_headers)
	FrequencyPenalty *float64
	MaxTokens        *int
	N                *int
	PresencePenalty  *float64
	ReasoningEffort  *string
	Seed             *int
	Temperature      *float64
	Timeout          *int
	TopP             *float64
}

// AzureOpenAIConfig holds Azure OpenAI configuration
type AzureOpenAIConfig struct {
	Model   string
	Headers map[string]string // Default headers to pass to Azure OpenAI API (matching Python default_headers)
	Timeout *int
}

// OpenAIModel implements model.LLM (see openai_adk.go) for OpenAI/Azure OpenAI.
type OpenAIModel struct {
	Config      *OpenAIConfig
	Client      *openai.Client
	AzureClient *openai.Client // For Azure OpenAI
	IsAzure     bool
	Logger      logr.Logger
}

// NewOpenAIModel creates a new OpenAI model instance
func NewOpenAIModel(config *OpenAIConfig) (*OpenAIModel, error) {
	return NewOpenAIModelWithLogger(config, logr.Logger{})
}

// NewOpenAIModelWithLogger creates a new OpenAI model instance with a logger
func NewOpenAIModelWithLogger(config *OpenAIConfig, logger logr.Logger) (*OpenAIModel, error) {
	apiKey := os.Getenv("OPENAI_API_KEY")
	if apiKey == "" {
		return nil, fmt.Errorf("OPENAI_API_KEY environment variable is not set")
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
		Model:   modelName,
		BaseUrl: baseURL,
		Headers: headers,
	}
	return newOpenAIModelFromConfig(config, apiKey, logger)
}

func newOpenAIModelFromConfig(config *OpenAIConfig, apiKey string, logger logr.Logger) (*OpenAIModel, error) {
	clientConfig := openai.DefaultConfig(apiKey)
	if config.BaseUrl != "" {
		clientConfig.BaseURL = config.BaseUrl
	}

	// Set timeout if specified, otherwise use default
	if config.Timeout != nil {
		clientConfig.HTTPClient.Timeout = time.Duration(*config.Timeout) * time.Second
	} else {
		clientConfig.HTTPClient.Timeout = DefaultExecutionTimeout
	}

	// Set default headers if provided (matching Python: default_headers=self.default_headers)
	if len(config.Headers) > 0 {
		originalTransport := clientConfig.HTTPClient.Transport
		if originalTransport == nil {
			originalTransport = http.DefaultTransport
		}
		clientConfig.HTTPClient.Transport = &headerTransport{
			base:    originalTransport,
			headers: config.Headers,
		}
		if logger.GetSink() != nil {
			logger.Info("Setting default headers for OpenAI client", "headersCount", len(config.Headers), "headers", config.Headers)
		}
	}

	client := openai.NewClientWithConfig(clientConfig)
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

// NewAzureOpenAIModelWithLogger creates a new Azure OpenAI model instance with a logger
func NewAzureOpenAIModelWithLogger(config *AzureOpenAIConfig, logger logr.Logger) (*OpenAIModel, error) {
	apiKey := os.Getenv("AZURE_OPENAI_API_KEY")
	azureEndpoint := os.Getenv("AZURE_OPENAI_ENDPOINT")
	apiVersion := os.Getenv("OPENAI_API_VERSION")
	if apiVersion == "" {
		apiVersion = "2024-02-15-preview"
	}
	if apiKey == "" {
		return nil, fmt.Errorf("AZURE_OPENAI_API_KEY environment variable is not set")
	}
	if azureEndpoint == "" {
		return nil, fmt.Errorf("AZURE_OPENAI_ENDPOINT environment variable is not set")
	}

	clientConfig := openai.DefaultAzureConfig(apiKey, azureEndpoint)
	clientConfig.APIVersion = apiVersion
	clientConfig.AzureModelMapperFunc = func(model string) string {
		if config.Model != "" {
			return config.Model
		}
		return model
	}
	if config.Timeout != nil {
		clientConfig.HTTPClient.Timeout = time.Duration(*config.Timeout) * time.Second
	} else {
		clientConfig.HTTPClient.Timeout = DefaultExecutionTimeout
	}
	if len(config.Headers) > 0 {
		originalTransport := clientConfig.HTTPClient.Transport
		if originalTransport == nil {
			originalTransport = http.DefaultTransport
		}
		clientConfig.HTTPClient.Transport = &headerTransport{
			base:    originalTransport,
			headers: config.Headers,
		}
	}
	client := openai.NewClientWithConfig(clientConfig)
	if logger.GetSink() != nil {
		logger.Info("Initialized Azure OpenAI model", "model", config.Model, "endpoint", azureEndpoint, "apiVersion", apiVersion)
	}
	return &OpenAIModel{
		Config:  &OpenAIConfig{Model: config.Model},
		Client:  client,
		IsAzure: true,
		Logger:  logger,
	}, nil
}

// headerTransport wraps an http.RoundTripper and adds custom headers to all requests
type headerTransport struct {
	base    http.RoundTripper
	headers map[string]string
}

func (t *headerTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	req = req.Clone(req.Context())
	for k, v := range t.headers {
		req.Header.Set(k, v)
	}
	return t.base.RoundTrip(req)
}
