package models

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-logr/logr"
	"github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/shared"
)

func TestNewAzureOpenAIModelWithLoggerRequiresEndpoint(t *testing.T) {
	t.Setenv("AZURE_OPENAI_ENDPOINT", "")

	_, err := NewAzureOpenAIModelWithLogger(context.Background(), &AzureOpenAIConfig{Model: "gpt-4o"}, logr.Discard())
	if err == nil || !strings.Contains(err.Error(), "AZURE_OPENAI_ENDPOINT environment variable is not set") {
		t.Fatalf("NewAzureOpenAIModelWithLogger() error = %v, want missing AZURE_OPENAI_ENDPOINT", err)
	}
}

// TestAzureOpenAIWorkloadIdentityEagerProbeFailsReadiness verifies that when no
// API key is set and the credential cannot acquire a token, model construction
// fails with an actionable error — failing agent readiness at startup instead of
// on the first request.
func TestAzureOpenAIWorkloadIdentityEagerProbeFailsReadiness(t *testing.T) {
	t.Setenv("AZURE_OPENAI_API_KEY", "")

	_, err := NewAzureOpenAIModelWithLogger(context.Background(), &AzureOpenAIConfig{
		Model:      "gpt-4o",
		Endpoint:   "https://example.openai.azure.com/",
		Deployment: "gpt-4o",
		APIVersion: "2024-02-15-preview",
		credential: erroringFoundryCredential{},
	}, logr.Discard())
	if err == nil || !strings.Contains(err.Error(), "no Azure credential resolved") {
		t.Fatalf("NewAzureOpenAIModelWithLogger() error = %v, want credential-not-resolved", err)
	}
}

// TestAzureOpenAIWorkloadIdentityEagerProbeSucceeds verifies the model is created
// when no API key is set and the credential can acquire a token at the cognitive
// services scope.
func TestAzureOpenAIWorkloadIdentityEagerProbeSucceeds(t *testing.T) {
	t.Setenv("AZURE_OPENAI_API_KEY", "")

	model, err := NewAzureOpenAIModelWithLogger(context.Background(), &AzureOpenAIConfig{
		Model:      "gpt-4o",
		Endpoint:   "https://example.openai.azure.com/",
		Deployment: "gpt-4o",
		APIVersion: "2024-02-15-preview",
		credential: &fakeFoundryCredential{t: t, token: "entra-token"},
	}, logr.Discard())
	if err != nil {
		t.Fatalf("NewAzureOpenAIModelWithLogger() error = %v", err)
	}
	if model == nil || !model.IsAzure {
		t.Fatalf("expected an Azure OpenAI model")
	}
}

// TestAzureOpenAIAPIKeySendsApiKeyHeader verifies that the Azure OpenAI model
// built through the shared azureai client sends the Api-Key header and hits the
// Azure deployment path with the configured api-version.
func TestAzureOpenAIAPIKeySendsApiKeyHeader(t *testing.T) {
	t.Setenv("AZURE_OPENAI_API_KEY", "test-key")

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
		fmt.Fprint(w, `{"id":"chatcmpl-test","object":"chat.completion","created":0,"model":"gpt-4o","choices":[{"index":0,"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}]}`)
	}))
	t.Cleanup(server.Close)

	model, err := NewAzureOpenAIModelWithLogger(context.Background(), &AzureOpenAIConfig{
		Model:      "gpt-4o",
		Endpoint:   server.URL,
		Deployment: "gpt-4o",
		APIVersion: "2024-06-01",
	}, logr.Discard())
	if err != nil {
		t.Fatalf("NewAzureOpenAIModelWithLogger() error = %v", err)
	}
	if !model.IsAzure {
		t.Fatalf("IsAzure = false, want true")
	}

	_, err = model.Client.Chat.Completions.New(context.Background(), openai.ChatCompletionNewParams{
		Model:    shared.ChatModel("gpt-4o"),
		Messages: []openai.ChatCompletionMessageParamUnion{openai.UserMessage("hello")},
	})
	if err != nil {
		t.Fatalf("chat completion error = %v", err)
	}

	got := <-reqs
	if got.apiKey != "test-key" {
		t.Fatalf("Api-Key = %q, want test-key", got.apiKey)
	}
	if got.path != "/openai/deployments/gpt-4o/chat/completions" {
		t.Fatalf("path = %q, want Azure deployment path", got.path)
	}
	if got.apiVersion != "2024-06-01" {
		t.Fatalf("api-version = %q, want 2024-06-01", got.apiVersion)
	}
}

// TestAzureOpenAIPassthroughInjectsBearerToken verifies that with
// APIKeyPassthrough enabled, the placeholder Api-Key is overwritten per request
// by the bearer token carried in the context.
func TestAzureOpenAIPassthroughInjectsBearerToken(t *testing.T) {
	reqs := make(chan string, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		reqs <- r.Header.Get("Api-Key")
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"id":"chatcmpl-test","object":"chat.completion","created":0,"model":"gpt-4o","choices":[{"index":0,"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}]}`)
	}))
	t.Cleanup(server.Close)

	cfg := &AzureOpenAIConfig{
		Model:      "gpt-4o",
		Endpoint:   server.URL,
		Deployment: "gpt-4o",
		APIVersion: "2024-06-01",
	}
	cfg.APIKeyPassthrough = true

	model, err := NewAzureOpenAIModelWithLogger(context.Background(), cfg, logr.Discard())
	if err != nil {
		t.Fatalf("NewAzureOpenAIModelWithLogger() error = %v", err)
	}

	ctx := context.WithValue(context.Background(), BearerTokenKey, "caller-token")
	_, err = model.Client.Chat.Completions.New(ctx, openai.ChatCompletionNewParams{
		Model:    shared.ChatModel("gpt-4o"),
		Messages: []openai.ChatCompletionMessageParamUnion{openai.UserMessage("hello")},
	}, openAIPassthroughOpts(ctx, model)...)
	if err != nil {
		t.Fatalf("chat completion error = %v", err)
	}

	if got := <-reqs; got != "caller-token" {
		t.Fatalf("Api-Key = %q, want caller-token", got)
	}
}
