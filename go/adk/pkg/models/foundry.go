package models

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"strings"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore"
	"github.com/Azure/azure-sdk-for-go/sdk/azcore/policy"
	"github.com/Azure/azure-sdk-for-go/sdk/azidentity"
	"github.com/go-logr/logr"
	"github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/option"
)

// foundryCognitiveServicesScope is the Azure data-plane scope used to request
// tokens for the Foundry / Azure AI Services OpenAI-compatible API.
const foundryCognitiveServicesScope = "https://cognitiveservices.azure.com/.default"

// FoundryConfig holds Azure AI Foundry configuration.
type FoundryConfig struct {
	TransportConfig
	Model      string
	Endpoint   string
	Deployment string
	APIVersion string
}

// NewFoundryModelWithLogger creates an Azure AI Foundry model.
//
// Foundry is driven through its OpenAI-compatible chat/completions data plane
// (POST {endpoint}/openai/deployments/{deployment}/chat/completions), which is
// a unified, multi-vendor surface: the deployment name selects the underlying
// model, so this single client reaches OpenAI models as well as other
// chat-completion models sold by Azure on Foundry (for example DeepSeek, Meta
// Llama, Mistral, Cohere, xAI Grok). It does not cover models that require a
// non-OpenAI wire format or a custom API (for example rerank, image, or
// time-series models). See:
//   - https://learn.microsoft.com/en-us/azure/ai-foundry/model-inference/concepts/endpoints
//   - https://learn.microsoft.com/en-us/azure/ai-foundry/foundry-models/concepts/models-sold-directly-by-azure
//
// Authentication is implicit: if FOUNDRY_API_KEY is set it is used as the
// data-plane API key; otherwise the model authenticates with
// DefaultAzureCredential, which resolves to Azure Workload Identity in-cluster
// (or the az CLI in local development).
func NewFoundryModelWithLogger(ctx context.Context, config *FoundryConfig, logger logr.Logger) (*OpenAIModel, error) {
	endpoint := config.Endpoint
	if endpoint == "" {
		endpoint = os.Getenv("FOUNDRY_ENDPOINT")
	}
	if endpoint == "" {
		return nil, fmt.Errorf("FOUNDRY_ENDPOINT environment variable is not set")
	}
	deployment := config.Deployment
	if deployment == "" {
		deployment = os.Getenv("FOUNDRY_DEPLOYMENT")
	}
	if deployment == "" {
		return nil, fmt.Errorf("FOUNDRY_DEPLOYMENT environment variable is not set")
	}
	apiVersion := config.APIVersion
	if apiVersion == "" {
		apiVersion = os.Getenv("FOUNDRY_API_VERSION")
	}
	if apiVersion == "" {
		apiVersion = "2024-10-21"
	}

	httpClient, err := BuildHTTPClient(config.TransportConfig)
	if err != nil {
		return nil, err
	}
	opts := []option.RequestOption{
		option.WithBaseURL(strings.TrimSuffix(endpoint, "/") + "/"),
		option.WithQueryAdd("api-version", apiVersion),
		option.WithMiddleware(azurePathRewriteMiddleware()),
		option.WithHTTPClient(httpClient),
	}

	// Implicit auth: use the API key when provided, otherwise fall back to
	// DefaultAzureCredential (Workload Identity in-cluster, az CLI in dev).
	if apiKey := os.Getenv("FOUNDRY_API_KEY"); apiKey != "" {
		opts = append(opts, option.WithHeader("Api-Key", apiKey))
	} else {
		credential, err := azidentity.NewDefaultAzureCredential(nil)
		if err != nil {
			return nil, fmt.Errorf("failed to create Azure credential: %w", err)
		}
		opts = append(opts,
			option.WithAPIKey("foundry-entra"),
			option.WithMiddleware(foundryBearerTokenMiddleware(credential)),
		)
	}

	client := openai.NewClient(opts...)
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

type foundryTokenCredential interface {
	GetToken(context.Context, policy.TokenRequestOptions) (azcore.AccessToken, error)
}

// foundryBearerTokenMiddleware implements the implicit Workload Identity auth
// path: when no FOUNDRY_API_KEY is set, it acquires an Azure AD bearer token
// from the provided credential (DefaultAzureCredential, which resolves to Azure
// Workload Identity in-cluster) and attaches it to each request, replacing the
// placeholder API key. This is distinct from API-key passthrough — the token
// comes from the workload's own identity, not from an incoming caller request.
func foundryBearerTokenMiddleware(credential foundryTokenCredential) option.Middleware {
	return func(r *http.Request, next option.MiddlewareNext) (*http.Response, error) {
		token, err := credential.GetToken(r.Context(), policy.TokenRequestOptions{Scopes: []string{foundryCognitiveServicesScope}})
		if err != nil {
			return nil, fmt.Errorf("failed to acquire Foundry token: %w", err)
		}
		r = r.Clone(r.Context())
		r.Header.Set("Authorization", "Bearer "+token.Token)
		return next(r)
	}
}
