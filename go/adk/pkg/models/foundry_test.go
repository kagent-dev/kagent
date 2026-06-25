package models

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore"
	"github.com/Azure/azure-sdk-for-go/sdk/azcore/policy"
	"github.com/go-logr/logr"
	"github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/shared"
)

func TestNewFoundryModelWithLoggerAPIKeyRequiresEnv(t *testing.T) {
	t.Setenv("FOUNDRY_API_KEY", "")

	_, err := NewFoundryModelWithLogger(context.Background(), &FoundryConfig{
		Endpoint:   "https://example.openai.azure.com/",
		Deployment: "gpt-4-1-nano",
		AuthType:   foundryAuthTypeAPIKey,
	}, logr.Discard())
	if err == nil || !strings.Contains(err.Error(), "FOUNDRY_API_KEY environment variable is not set") {
		t.Fatalf("NewFoundryModelWithLogger() error = %v, want missing FOUNDRY_API_KEY", err)
	}
}

func TestNewFoundryModelWithLoggerAPIKeyPassthrough(t *testing.T) {
	model, err := NewFoundryModelWithLogger(context.Background(), &FoundryConfig{
		Endpoint:   "https://example.openai.azure.com/",
		Deployment: "gpt-4-1-nano",
		AuthType:   foundryAuthTypeAPIKeyPassthrough,
	}, logr.Discard())
	if err != nil {
		t.Fatalf("NewFoundryModelWithLogger() error = %v", err)
	}
	if model == nil || model.Config == nil || !model.Config.APIKeyPassthrough {
		t.Fatalf("APIKeyPassthrough = false, want true")
	}
	if !model.IsAzure {
		t.Fatalf("IsAzure = false, want true")
	}
}

func TestFoundryAPIKeyPassthroughSendsAuthorizationHeader(t *testing.T) {
	requests := make(chan foundryRequest, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests <- foundryRequest{
			apiKey:        r.Header.Get("Api-Key"),
			authorization: r.Header.Get("Authorization"),
			path:          r.URL.Path,
			apiVersion:    r.URL.Query().Get("api-version"),
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"id":"chatcmpl-test","object":"chat.completion","created":0,"model":"gpt-4-1-nano","choices":[{"index":0,"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}]}`)
	}))
	t.Cleanup(server.Close)

	model, err := NewFoundryModelWithLogger(context.Background(), &FoundryConfig{
		Endpoint:   server.URL,
		Deployment: "gpt-4-1-nano",
		AuthType:   foundryAuthTypeAPIKeyPassthrough,
	}, logr.Discard())
	if err != nil {
		t.Fatalf("NewFoundryModelWithLogger() error = %v", err)
	}

	ctx := context.WithValue(context.Background(), BearerTokenKey, "incoming-token")
	_, err = model.Client.Chat.Completions.New(ctx, openai.ChatCompletionNewParams{
		Model:    shared.ChatModel("gpt-4-1-nano"),
		Messages: []openai.ChatCompletionMessageParamUnion{openai.UserMessage("hello")},
	}, openAIPassthroughOpts(ctx, model)...)
	if err != nil {
		t.Fatalf("Chat completion request error = %v", err)
	}
	req := <-requests
	if req.path != "/openai/deployments/gpt-4-1-nano/chat/completions" {
		t.Fatalf("path = %q, want Azure deployment path", req.path)
	}
	if req.apiVersion != "2024-10-21" {
		t.Fatalf("api-version = %q, want 2024-10-21", req.apiVersion)
	}
	if req.authorization != "Bearer incoming-token" {
		t.Fatalf("Authorization header = %q, want Bearer incoming-token", req.authorization)
	}
	if req.apiKey != "" {
		t.Fatalf("Api-Key header = %q, want empty", req.apiKey)
	}
}

func TestFoundryBearerTokenMiddlewareUsesRequestContext(t *testing.T) {
	credential := &requestContextCredential{t: t}
	middleware := foundryBearerTokenMiddleware(credential)
	req := httptest.NewRequest(http.MethodPost, "https://example.com/chat/completions", nil)
	req = req.WithContext(context.WithValue(req.Context(), foundryRequestContextKey{}, "request-context"))

	_, err := middleware(req, func(r *http.Request) (*http.Response, error) {
		if got := r.Header.Get("Authorization"); got != "Bearer request-token" {
			t.Fatalf("Authorization = %q, want bearer token", got)
		}
		return &http.Response{StatusCode: http.StatusOK, Body: http.NoBody}, nil
	})
	if err != nil {
		t.Fatalf("middleware error = %v", err)
	}
}

func TestNewFoundryModelWithLoggerUnsupportedAuthType(t *testing.T) {
	_, err := NewFoundryModelWithLogger(context.Background(), &FoundryConfig{
		Endpoint:   "https://example.openai.azure.com/",
		Deployment: "gpt-4-1-nano",
		AuthType:   "Unknown",
	}, logr.Discard())
	if err == nil || !strings.Contains(err.Error(), "unsupported Foundry auth type: Unknown") {
		t.Fatalf("NewFoundryModelWithLogger() error = %v, want unsupported auth type", err)
	}
}

type foundryRequestContextKey struct{}

type foundryRequest struct {
	apiKey        string
	authorization string
	path          string
	apiVersion    string
}

type requestContextCredential struct {
	t *testing.T
}

func (c *requestContextCredential) GetToken(ctx context.Context, opts policy.TokenRequestOptions) (azcore.AccessToken, error) {
	c.t.Helper()
	if got := ctx.Value(foundryRequestContextKey{}); got != "request-context" {
		c.t.Fatalf("GetToken context marker = %v, want request-context", got)
	}
	if len(opts.Scopes) != 1 || opts.Scopes[0] != "https://cognitiveservices.azure.com/.default" {
		c.t.Fatalf("Scopes = %v, want cognitive services scope", opts.Scopes)
	}
	return azcore.AccessToken{Token: "request-token"}, nil
}
