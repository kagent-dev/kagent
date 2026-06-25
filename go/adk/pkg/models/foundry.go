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

// FoundryConfig holds Foundry configuration.
type FoundryConfig struct {
	TransportConfig
	Model      string
	Endpoint   string
	Deployment string
	APIVersion string
	AuthType   string
}

const (
	foundryAuthTypeAPIKey            = "APIKey"
	foundryAuthTypeWorkloadIdentity  = "WorkloadIdentity"
	foundryAuthTypeAPIKeyPassthrough = "APIKeyPassthrough"
)

// NewFoundryModelWithLogger creates a Foundry model.
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

	authType := config.AuthType
	if authType == "" {
		authType = foundryAuthTypeWorkloadIdentity
	}
	switch authType {
	case foundryAuthTypeAPIKey:
		apiKey := os.Getenv("FOUNDRY_API_KEY")
		if apiKey == "" {
			return nil, fmt.Errorf("FOUNDRY_API_KEY environment variable is not set")
		}
		opts = append(opts, option.WithHeader("Api-Key", apiKey))
	case foundryAuthTypeWorkloadIdentity:
		credential, err := azidentity.NewDefaultAzureCredential(nil)
		if err != nil {
			return nil, fmt.Errorf("failed to create Azure credential: %w", err)
		}
		opts = append(opts,
			option.WithAPIKey("foundry-entra"),
			option.WithMiddleware(foundryBearerTokenMiddleware(credential)),
		)
	case foundryAuthTypeAPIKeyPassthrough:
		config.APIKeyPassthrough = true
		opts = append(opts, option.WithMiddleware(foundryPassthroughBearerTokenMiddleware()))
	default:
		return nil, fmt.Errorf("unsupported Foundry auth type: %s", authType)
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

func foundryBearerTokenMiddleware(credential foundryTokenCredential) option.Middleware {
	return func(r *http.Request, next option.MiddlewareNext) (*http.Response, error) {
		token, err := credential.GetToken(r.Context(), policy.TokenRequestOptions{Scopes: []string{"https://cognitiveservices.azure.com/.default"}})
		if err != nil {
			return nil, fmt.Errorf("failed to acquire Foundry token: %w", err)
		}
		r = r.Clone(r.Context())
		r.Header.Set("Authorization", "Bearer "+token.Token)
		return next(r)
	}
}

func foundryPassthroughBearerTokenMiddleware() option.Middleware {
	return func(r *http.Request, next option.MiddlewareNext) (*http.Response, error) {
		token, ok := r.Context().Value(BearerTokenKey).(string)
		if !ok || token == "" {
			return next(r)
		}
		r = r.Clone(r.Context())
		r.Header.Del("Api-Key")
		r.Header.Set("Authorization", "Bearer "+token)
		return next(r)
	}
}
