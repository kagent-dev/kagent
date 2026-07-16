package models

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/go-logr/logr"
	"github.com/kagent-dev/kagent/go/adk/pkg/internal/azureai"
)

// FoundryConfig holds Azure AI Foundry configuration.
type FoundryConfig struct {
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

// NewFoundryModelWithLogger creates a model for the Azure AI Foundry
// OpenAI-compatible surface.
//
// This constructor targets Foundry's OpenAI-compatible chat/completions data
// plane (POST {endpoint}/openai/deployments/{deployment}/chat/completions). That
// surface is multi-vendor: the deployment name selects the underlying model, so
// this single client reaches OpenAI models as well as the non-OpenAI
// chat-completion models Azure sells directly on Foundry (for example DeepSeek,
// Meta Llama, Mistral, Cohere, xAI Grok). It does not cover models served through
// a different wire surface — notably Claude, which uses the Anthropic Messages
// API (planned as a separate azureai.NewAnthropicClient) — nor non-chat models
// such as rerank, image, or time-series. See:
//   - https://learn.microsoft.com/en-us/azure/ai-foundry/model-inference/concepts/endpoints
//   - https://learn.microsoft.com/en-us/azure/ai-foundry/foundry-models/concepts/models-sold-directly-by-azure
//
// Authentication is implicit: if FOUNDRY_API_KEY is set it is used as the
// data-plane API key; otherwise the model authenticates with
// DefaultAzureCredential, which resolves to Azure Workload Identity in-cluster
// (or the az CLI in local development).
func NewFoundryModelWithLogger(ctx context.Context, config *FoundryConfig, logger logr.Logger) (*OpenAIModel, error) {
	endpoint, deployment, apiVersion := azureai.ResolveFoundry(config.Endpoint, config.Deployment, config.APIVersion)
	if endpoint == "" {
		return nil, fmt.Errorf("FOUNDRY_ENDPOINT environment variable is not set")
	}
	if deployment == "" {
		return nil, fmt.Errorf("FOUNDRY_DEPLOYMENT environment variable is not set")
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

	// Implicit auth: use the API key when provided, otherwise fall back to
	// DefaultAzureCredential (Workload Identity in-cluster, az CLI in dev).
	if apiKey := os.Getenv(azureai.FoundryAPIKeyEnvVar); apiKey != "" {
		clientCfg.APIKey = apiKey
		if logger.GetSink() != nil {
			logger.Info("Foundry authenticating with API key", "deployment", deployment)
		}
	} else {
		credential := config.credential
		if credential == nil {
			credential, err = azureai.NewDefaultCredential()
			if err != nil {
				return nil, fmt.Errorf("failed to create Azure credential for Foundry: %w", err)
			}
		}
		// Eagerly acquire a token so a missing or misconfigured Workload Identity
		// fails readiness at startup with an actionable error, instead of failing
		// silently on the first inference request.
		if _, err := azureai.AcquireToken(ctx, credential); err != nil {
			return nil, fmt.Errorf("no Foundry credential resolved: set an API key (apiKeySecret / FOUNDRY_API_KEY) or configure Azure Workload Identity (pod label + ServiceAccount annotation + federated credential): %w", err)
		}
		clientCfg.Credential = credential
		if logger.GetSink() != nil {
			logger.Info("Foundry authenticating with DefaultAzureCredential (Workload Identity)", "deployment", deployment)
		}
	}

	// A future apiFormat=anthropic discriminator on the ModelConfig would branch
	// here to azureai.NewAnthropicClient (the Anthropic Messages surface, reusing
	// the same azureai credential + token helpers).
	client, err := azureai.NewOpenAIClient(clientCfg)
	if err != nil {
		return nil, err
	}
	if logger.GetSink() != nil {
		logger.Info("Initialized Foundry model", "model", config.Model, "deployment", deployment, "endpoint", endpoint, "apiVersion", apiVersion)
	}
	return &OpenAIModel{
		Config: &OpenAIConfig{
			TransportConfig: config.TransportConfig,
			Model:           deployment,
			BaseUrl:         strings.TrimSuffix(endpoint, "/") + "/",
		},
		Client:  client,
		IsAzure: true,
		Logger:  logger,
	}, nil
}
