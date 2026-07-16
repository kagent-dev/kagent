// Package azureai contains helpers shared across the Azure AI Services data plane
// (Azure AI Foundry and Azure OpenAI): Azure AD credential construction, token
// acquisition, the bearer-token middleware, and constructors for the concrete
// SDK clients that speak each Azure AI wire surface.
//
// It is internal to adk/pkg so it can be shared by the model (adk/pkg/models) and
// embedding (adk/pkg/embedding) packages without exposing an Azure-specific
// surface outside the ADK.
package azureai

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"strings"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore"
	"github.com/Azure/azure-sdk-for-go/sdk/azcore/policy"
	"github.com/Azure/azure-sdk-for-go/sdk/azidentity"
	"github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/option"
)

// CognitiveServicesScope is the Azure data-plane scope used to request AAD tokens
// for the Azure AI Services (Cognitive Services) data plane. It is shared by the
// OpenAI-compatible and (future) Anthropic surfaces.
const CognitiveServicesScope = "https://cognitiveservices.azure.com/.default"

// Foundry-specific configuration conventions. These are the environment variables
// the controller injects for a Foundry ModelConfig plus the default api-version.
const (
	FoundryEndpointEnvVar   = "FOUNDRY_ENDPOINT"
	FoundryDeploymentEnvVar = "FOUNDRY_DEPLOYMENT"
	FoundryAPIVersionEnvVar = "FOUNDRY_API_VERSION"
	FoundryAPIKeyEnvVar     = "FOUNDRY_API_KEY"
)

// FoundryDefaultAPIVersion is the Foundry OpenAI-compatible data-plane API
// version used when none is configured.
const FoundryDefaultAPIVersion = "2024-10-21"

// ResolveFoundry applies FOUNDRY_* environment-variable fallbacks and the default
// api-version. Empty endpoint/deployment are returned as-is so callers can
// produce context-specific validation errors.
func ResolveFoundry(endpoint, deployment, apiVersion string) (ep, dep, ver string) {
	ep = endpoint
	if ep == "" {
		ep = os.Getenv(FoundryEndpointEnvVar)
	}
	dep = deployment
	if dep == "" {
		dep = os.Getenv(FoundryDeploymentEnvVar)
	}
	ver = apiVersion
	if ver == "" {
		ver = os.Getenv(FoundryAPIVersionEnvVar)
	}
	if ver == "" {
		ver = FoundryDefaultAPIVersion
	}
	return ep, dep, ver
}

// TokenCredential is the minimal Azure credential surface used for the implicit
// Workload Identity auth path. It is satisfied by azidentity credentials and by
// test fakes.
type TokenCredential interface {
	GetToken(context.Context, policy.TokenRequestOptions) (azcore.AccessToken, error)
}

// NewDefaultCredential constructs an Azure DefaultAzureCredential, which resolves
// to Azure Workload Identity in-cluster (or the az CLI in local development).
func NewDefaultCredential() (TokenCredential, error) {
	return azidentity.NewDefaultAzureCredential(nil)
}

// AcquireToken fetches a bearer token for the Azure AI Services data-plane scope.
func AcquireToken(ctx context.Context, cred TokenCredential) (string, error) {
	token, err := cred.GetToken(ctx, policy.TokenRequestOptions{Scopes: []string{CognitiveServicesScope}})
	if err != nil {
		return "", err
	}
	return token.Token, nil
}

// BearerTokenMiddleware implements the implicit Workload Identity auth path: it
// acquires an Azure AD bearer token from the credential (DefaultAzureCredential,
// which resolves to Azure Workload Identity in-cluster) and attaches it to each
// request, replacing the placeholder API key. This is distinct from API-key
// passthrough — the token comes from the workload's own identity, not from an
// incoming caller request.
func BearerTokenMiddleware(cred TokenCredential) option.Middleware {
	return func(r *http.Request, next option.MiddlewareNext) (*http.Response, error) {
		token, err := AcquireToken(r.Context(), cred)
		if err != nil {
			return nil, fmt.Errorf("failed to acquire Azure AI token: %w", err)
		}
		r = r.Clone(r.Context())
		r.Header.Set("Authorization", "Bearer "+token)
		return next(r)
	}
}

// ClientConfig configures a client for the Azure AI Services OpenAI-compatible
// data plane.
type ClientConfig struct {
	// Endpoint is the Azure AI Services account endpoint, e.g.
	// https://<account>.cognitiveservices.azure.com.
	Endpoint string
	// Deployment is the deployment name, placed in the data-plane URL path.
	Deployment string
	// APIVersion is the data-plane api-version query parameter.
	APIVersion string
	// APIKey, when set, authenticates via the Api-Key header.
	APIKey string
	// Credential authenticates via an Azure AD bearer token when APIKey is empty.
	Credential TokenCredential
	// HTTPClient is the transport used by the client. Defaults to
	// http.DefaultClient when nil.
	HTTPClient *http.Client
}

// NewOpenAIClient builds an openai-go client for the Azure AI Services
// OpenAI-compatible surface (chat + embeddings), rooted at
// {endpoint}/openai/deployments/{deployment}/ with the api-version query and
// implicit auth: the Api-Key header when APIKey is set, otherwise an Azure AD
// bearer token from Credential.
//
// A NewAnthropicClient for the Azure AI Foundry Anthropic (Claude) surface is
// planned and will live alongside this constructor, reusing the same credential
// and token helpers.
func NewOpenAIClient(cfg ClientConfig) (openai.Client, error) {
	if cfg.Endpoint == "" {
		return openai.Client{}, fmt.Errorf("endpoint is required")
	}
	if cfg.Deployment == "" {
		return openai.Client{}, fmt.Errorf("deployment is required")
	}
	if cfg.APIKey == "" && cfg.Credential == nil {
		return openai.Client{}, fmt.Errorf("an API key or Azure credential is required")
	}

	httpClient := cfg.HTTPClient
	if httpClient == nil {
		httpClient = http.DefaultClient
	}

	baseURL := strings.TrimSuffix(cfg.Endpoint, "/") + "/openai/deployments/" + url.PathEscape(cfg.Deployment) + "/"
	opts := []option.RequestOption{
		option.WithBaseURL(baseURL),
		option.WithQueryAdd("api-version", cfg.APIVersion),
		option.WithHTTPClient(httpClient),
	}
	if cfg.APIKey != "" {
		opts = append(opts, option.WithHeader("Api-Key", cfg.APIKey))
	} else {
		// Workload Identity auth. The openai-go SDK refuses to send a request
		// without a non-empty API key, so we pass a placeholder that is never
		// actually used: the bearer middleware runs on every request and
		// overwrites the SDK's Authorization header with a freshly acquired Azure
		// AD (Entra) token from the credential. Acquiring the token per request —
		// rather than setting it once as the API key — keeps auth valid as the
		// token expires and is refreshed by the credential.
		opts = append(opts,
			option.WithAPIKey("azure-entra"),
			option.WithMiddleware(BearerTokenMiddleware(cfg.Credential)),
		)
	}
	return openai.NewClient(opts...), nil
}
