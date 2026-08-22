package models

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/go-logr/logr"
	"github.com/kagent-dev/kagent/go/adk/pkg/internal/azureai"
)

// FoundryConfig holds Azure AI Foundry configuration for the OpenAI-compatible
// surface. The Anthropic (Claude) surface has no Foundry-specific config type; it
// reuses AnthropicConfig via NewFoundryAnthropicModelWithLogger.
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
// Meta Llama, Mistral, Cohere, xAI Grok). Claude uses the Anthropic Messages API
// instead and is handled by NewFoundryAnthropicModelWithLogger.
//
// Authentication is implicit: the incoming bearer token when APIKeyPassthrough is
// enabled; otherwise FOUNDRY_API_KEY when set; otherwise DefaultAzureCredential,
// which resolves to Azure Workload Identity in-cluster (or the az CLI in local
// development).
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

	// Implicit auth: the incoming bearer token when APIKeyPassthrough is enabled
	// (a placeholder Api-Key is overwritten per request by openAIPassthroughOpts),
	// otherwise the API key when provided, otherwise DefaultAzureCredential
	// (Workload Identity in-cluster, az CLI in dev), eagerly probed so a
	// misconfigured identity fails readiness at startup.
	apiKey := os.Getenv(azureai.FoundryAPIKeyEnvVar)
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

	// Claude (apiFormat=anthropic) never reaches here: agent.go's model dispatch
	// routes it to NewFoundryAnthropicModelWithLogger, so build the OpenAI client.
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

// NewFoundryAnthropicModelWithLogger creates a model for Claude models hosted on
// Azure AI Foundry, which are served over the Anthropic Messages API rather than
// the OpenAI-compatible surface used by NewFoundryModelWithLogger.
//
// The request targets {endpoint}/anthropic/v1/messages; the deployment name is
// sent in the request's model field. Authentication is implicit and mirrors the
// Foundry OpenAI model: the incoming bearer token when APIKeyPassthrough is
// enabled; otherwise FOUNDRY_API_KEY (sent as x-api-key); otherwise the Azure
// credential, scoped to azureai.AIFoundryScope and eagerly probed so a
// misconfigured identity fails readiness at startup.
//
// cred is the Azure credential for the Workload Identity path; like the region /
// projectID arguments on the Vertex and Bedrock constructors it is passed
// explicitly rather than stored on the shared AnthropicConfig. It is nil in
// production (resolved to DefaultAzureCredential — Workload Identity in-cluster,
// or the az CLI in local development) and set by tests to inject a fake.
//
// It returns an *AnthropicModel so the shared genai<->Messages translation in
// anthropic_adk.go is reused unchanged.
func NewFoundryAnthropicModelWithLogger(ctx context.Context, config *AnthropicConfig, endpoint, deployment string, cred azureai.TokenCredential, logger logr.Logger) (*AnthropicModel, error) {
	// api-version is unused on the Messages surface (the SDK sets the
	// anthropic-version header), so it is resolved and discarded.
	ep, dep, _ := azureai.ResolveFoundry(endpoint, deployment, "")
	if ep == "" {
		return nil, fmt.Errorf("FOUNDRY_ENDPOINT environment variable is not set")
	}
	if dep == "" {
		return nil, fmt.Errorf("FOUNDRY_DEPLOYMENT environment variable is not set")
	}

	httpClient, err := BuildHTTPClient(config.TransportConfig)
	if err != nil {
		return nil, err
	}

	clientCfg := azureai.AnthropicClientConfig{
		Endpoint:   ep,
		EntraScope: azureai.AIFoundryScope,
		HTTPClient: httpClient,
	}

	apiKey := os.Getenv(azureai.FoundryAPIKeyEnvVar)
	if config.APIKeyPassthrough {
		// Placeholder x-api-key overwritten per request by anthropicPassthroughOpts.
		apiKey = "passthrough"
	}
	// Shared implicit auth: the API key when set, otherwise an Azure credential
	// (eagerly probed at the AI Foundry scope so a bad identity fails readiness).
	resolvedKey, resolvedCred, err := azureai.ResolveImplicitAuth(ctx, azureai.AuthOptions{
		APIKey:     apiKey,
		Credential: cred,
		EagerProbe: true,
		EntraScope: azureai.AIFoundryScope,
	})
	if err != nil {
		return nil, err
	}
	clientCfg.APIKey = resolvedKey
	clientCfg.Credential = resolvedCred

	client, err := azureai.NewAnthropicClient(clientCfg)
	if err != nil {
		return nil, err
	}

	if logger.GetSink() != nil {
		logger.Info("Initialized Foundry Anthropic model", "deployment", dep, "endpoint", ep)
	}

	modelConfig := *config
	modelConfig.Model = dep

	return &AnthropicModel{
		Config: &modelConfig,
		Client: client,
		Logger: logger,
	}, nil
}
