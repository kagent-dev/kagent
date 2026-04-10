package models

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"

	"github.com/go-logr/logr"
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
	Model string
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
// Uses Azure-style base URL, Api-Key header, and path rewriting so we do not depend on the azure package.
func NewAzureOpenAIModelWithLogger(config *AzureOpenAIConfig, logger logr.Logger) (*OpenAIModel, error) {
	apiVersion := os.Getenv("OPENAI_API_VERSION")
	if apiVersion == "" {
		apiVersion = "2024-02-15-preview"
	}

	// Handle API key - use placeholder for passthrough
	apiKey := "passthrough" // placeholder; real auth set per-request by transport
	azureEndpoint := ""
	if !config.APIKeyPassthrough {
		apiKey = os.Getenv("AZURE_OPENAI_API_KEY")
		azureEndpoint = os.Getenv("AZURE_OPENAI_ENDPOINT")
		if apiKey == "" {
			return nil, fmt.Errorf("AZURE_OPENAI_API_KEY environment variable is not set")
		}
		if azureEndpoint == "" {
			return nil, fmt.Errorf("AZURE_OPENAI_ENDPOINT environment variable is not set")
		}
	} else {
		// For passthrough mode, we still need the endpoint
		azureEndpoint = os.Getenv("AZURE_OPENAI_ENDPOINT")
		if azureEndpoint == "" {
			return nil, fmt.Errorf("AZURE_OPENAI_ENDPOINT environment variable is not set")
		}
	}

	baseURL := strings.TrimSuffix(azureEndpoint, "/") + "/"
	opts := []option.RequestOption{
		option.WithBaseURL(baseURL),
		option.WithQueryAdd("api-version", apiVersion),
		option.WithHeader("Api-Key", apiKey),
		option.WithMiddleware(azurePathRewriteMiddleware()),
	}
	httpClient, err := BuildHTTPClient(config.TransportConfig)
	if err != nil {
		return nil, err
	}
	opts = append(opts, option.WithHTTPClient(httpClient))

	client := openai.NewClient(opts...)
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

// azurePathRewriteMiddleware rewrites .../chat/completions to .../openai/deployments/{model}/chat/completions
// by reading the request body for the model field (Azure deployment name).
// Preserves the path prefix (e.g. /api/v1/proxy/) so proxies with a base path still work.
func azurePathRewriteMiddleware() option.Middleware {
	return func(r *http.Request, next option.MiddlewareNext) (*http.Response, error) {
		pathSuffix := strings.TrimPrefix(r.URL.Path, "/")
		var suffix string
		switch {
		case strings.HasSuffix(pathSuffix, "chat/completions"):
			suffix = "chat/completions"
		case strings.HasSuffix(pathSuffix, "completions"):
			suffix = "completions"
		case strings.HasSuffix(pathSuffix, "embeddings"):
			suffix = "embeddings"
		default:
			return next(r)
		}
		if r.Body == nil {
			return next(r)
		}
		var buf bytes.Buffer
		if _, err := buf.ReadFrom(r.Body); err != nil {
			return nil, err
		}
		r.Body = io.NopCloser(&buf)
		var payload struct {
			Model string `json:"model"`
		}
		if err := json.NewDecoder(bytes.NewReader(buf.Bytes())).Decode(&payload); err != nil || payload.Model == "" {
			r.Body = io.NopCloser(bytes.NewReader(buf.Bytes()))
			return next(r)
		}
		deployment := url.PathEscape(payload.Model)
		// Keep base path (e.g. /api/v1/proxy), replace suffix with Azure-style path
		basePath := strings.TrimSuffix(r.URL.Path, suffix)
		basePath = strings.TrimRight(basePath, "/")
		r.URL.Path = basePath + "/openai/deployments/" + deployment + "/" + suffix
		r.Body = io.NopCloser(bytes.NewReader(buf.Bytes()))
		return next(r)
	}
}
