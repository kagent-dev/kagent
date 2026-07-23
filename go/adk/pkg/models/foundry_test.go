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
	"github.com/kagent-dev/kagent/go/adk/pkg/internal/azureai"
	"github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/shared"
)

func TestNewFoundryModelWithLoggerRequiresEndpoint(t *testing.T) {
	t.Setenv("FOUNDRY_ENDPOINT", "")

	_, err := NewFoundryModelWithLogger(context.Background(), &FoundryConfig{
		Deployment: "gpt-4-1-nano",
	}, logr.Discard())
	if err == nil || !strings.Contains(err.Error(), "FOUNDRY_ENDPOINT environment variable is not set") {
		t.Fatalf("NewFoundryModelWithLogger() error = %v, want missing FOUNDRY_ENDPOINT", err)
	}
}

// TestFoundryAPIKeySendsApiKeyHeader verifies the implicit API-key path: when
// FOUNDRY_API_KEY is set, requests carry the Api-Key header and hit the Azure
// deployment path with the api-version query parameter.
func TestFoundryAPIKeySendsApiKeyHeader(t *testing.T) {
	t.Setenv("FOUNDRY_API_KEY", "test-key")

	type captured struct {
		apiKey     string
		path       string
		apiVersion string
	}
	reqs := make(chan captured, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		reqs <- captured{
			apiKey:     r.Header.Get("Api-Key"),
			path:       r.URL.Path,
			apiVersion: r.URL.Query().Get("api-version"),
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"id":"chatcmpl-test","object":"chat.completion","created":0,"model":"gpt-4-1-nano","choices":[{"index":0,"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}]}`)
	}))
	t.Cleanup(server.Close)

	model, err := NewFoundryModelWithLogger(context.Background(), &FoundryConfig{
		Endpoint:   server.URL,
		Deployment: "gpt-4-1-nano",
	}, logr.Discard())
	if err != nil {
		t.Fatalf("NewFoundryModelWithLogger() error = %v", err)
	}
	if !model.IsAzure {
		t.Fatalf("IsAzure = false, want true")
	}

	_, err = model.Client.Chat.Completions.New(context.Background(), openai.ChatCompletionNewParams{
		Model:    shared.ChatModel("gpt-4-1-nano"),
		Messages: []openai.ChatCompletionMessageParamUnion{openai.UserMessage("hello")},
	})
	if err != nil {
		t.Fatalf("chat completion error = %v", err)
	}

	got := <-reqs
	if got.apiKey != "test-key" {
		t.Fatalf("Api-Key = %q, want test-key", got.apiKey)
	}
	if got.path != "/openai/deployments/gpt-4-1-nano/chat/completions" {
		t.Fatalf("path = %q, want Azure deployment path", got.path)
	}
	if got.apiVersion != "2024-10-21" {
		t.Fatalf("api-version = %q, want 2024-10-21", got.apiVersion)
	}
}

type fakeFoundryCredential struct {
	t     *testing.T
	token string
}

func (c *fakeFoundryCredential) GetToken(_ context.Context, opts policy.TokenRequestOptions) (azcore.AccessToken, error) {
	c.t.Helper()
	if len(opts.Scopes) != 1 || opts.Scopes[0] != azureai.CognitiveServicesScope {
		c.t.Fatalf("Scopes = %v, want cognitive services scope", opts.Scopes)
	}
	return azcore.AccessToken{Token: c.token}, nil
}

type erroringFoundryCredential struct{}

func (erroringFoundryCredential) GetToken(context.Context, policy.TokenRequestOptions) (azcore.AccessToken, error) {
	return azcore.AccessToken{}, fmt.Errorf("no ambient Azure credential")
}

// TestFoundryWorkloadIdentityEagerProbeFailsReadiness verifies that when no API
// key is set and the credential cannot acquire a token, model construction fails
// with an actionable error — which fails agent readiness at startup instead of
// failing silently on the first request.
func TestFoundryWorkloadIdentityEagerProbeFailsReadiness(t *testing.T) {
	t.Setenv("FOUNDRY_API_KEY", "")

	_, err := NewFoundryModelWithLogger(context.Background(), &FoundryConfig{
		Endpoint:   "https://example.cognitiveservices.azure.com/",
		Deployment: "gpt-4-1-nano",
		credential: erroringFoundryCredential{},
	}, logr.Discard())
	if err == nil || !strings.Contains(err.Error(), "no Azure credential resolved") {
		t.Fatalf("NewFoundryModelWithLogger() error = %v, want credential-not-resolved", err)
	}
}

// TestFoundryWorkloadIdentityEagerProbeSucceeds verifies the model is created
// when the credential can acquire a token at the cognitive services scope.
func TestFoundryWorkloadIdentityEagerProbeSucceeds(t *testing.T) {
	t.Setenv("FOUNDRY_API_KEY", "")

	model, err := NewFoundryModelWithLogger(context.Background(), &FoundryConfig{
		Endpoint:   "https://example.cognitiveservices.azure.com/",
		Deployment: "gpt-4-1-nano",
		credential: &fakeFoundryCredential{t: t, token: "entra-token"},
	}, logr.Discard())
	if err != nil {
		t.Fatalf("NewFoundryModelWithLogger() error = %v", err)
	}
	if model == nil || !model.IsAzure {
		t.Fatalf("expected an Azure Foundry model")
	}
}
