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
	"github.com/anthropics/anthropic-sdk-go"
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

// TestFoundryPassthroughInjectsBearerToken verifies that with APIKeyPassthrough
// enabled, the placeholder Api-Key is overwritten per request by the bearer token
// carried in the context.
func TestFoundryPassthroughInjectsBearerToken(t *testing.T) {
	t.Setenv("FOUNDRY_API_KEY", "")

	reqs := make(chan string, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		reqs <- r.Header.Get("Api-Key")
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"id":"chatcmpl-test","object":"chat.completion","created":0,"model":"gpt-4-1-nano","choices":[{"index":0,"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}]}`)
	}))
	t.Cleanup(server.Close)

	cfg := &FoundryConfig{
		Model:      "gpt-4.1-nano",
		Endpoint:   server.URL,
		Deployment: "gpt-4-1-nano",
		APIVersion: "2024-10-21",
	}
	cfg.APIKeyPassthrough = true

	model, err := NewFoundryModelWithLogger(context.Background(), cfg, logr.Discard())
	if err != nil {
		t.Fatalf("NewFoundryModelWithLogger() error = %v", err)
	}

	ctx := context.WithValue(context.Background(), BearerTokenKey, "caller-token")
	_, err = model.Client.Chat.Completions.New(ctx, openai.ChatCompletionNewParams{
		Model:    shared.ChatModel("gpt-4-1-nano"),
		Messages: []openai.ChatCompletionMessageParamUnion{openai.UserMessage("hello")},
	}, openAIPassthroughOpts(ctx, model)...)
	if err != nil {
		t.Fatalf("chat completion error = %v", err)
	}

	if got := <-reqs; got != "caller-token" {
		t.Fatalf("Api-Key = %q, want caller-token", got)
	}
}

// TestFoundryPassthroughSkipsCredentialProbe verifies that enabling passthrough
// bypasses the Workload Identity eager token probe: construction succeeds even
// with a credential that can never acquire a token.
func TestFoundryPassthroughSkipsCredentialProbe(t *testing.T) {
	t.Setenv("FOUNDRY_API_KEY", "")

	cfg := &FoundryConfig{
		Model:      "gpt-4.1-nano",
		Endpoint:   "https://example.cognitiveservices.azure.com/",
		Deployment: "gpt-4-1-nano",
		credential: erroringFoundryCredential{},
	}
	cfg.APIKeyPassthrough = true

	model, err := NewFoundryModelWithLogger(context.Background(), cfg, logr.Discard())
	if err != nil {
		t.Fatalf("NewFoundryModelWithLogger() error = %v, want success (passthrough must not probe the credential)", err)
	}
	if model == nil || !model.IsAzure {
		t.Fatalf("expected an Azure Foundry model")
	}
}

const foundryAnthropicMessageResponse = `{"id":"msg_1","type":"message","role":"assistant","model":"claude-haiku-4-5","content":[{"type":"text","text":"ok"}],"stop_reason":"end_turn","usage":{"input_tokens":1,"output_tokens":1}}`

func TestNewFoundryAnthropicModelRequiresEndpoint(t *testing.T) {
	t.Setenv("FOUNDRY_ENDPOINT", "")

	_, err := NewFoundryAnthropicModelWithLogger(context.Background(), &AnthropicConfig{}, "", "claude-haiku-4-5", nil, logr.Discard())
	if err == nil || !strings.Contains(err.Error(), "FOUNDRY_ENDPOINT environment variable is not set") {
		t.Fatalf("NewFoundryAnthropicModelWithLogger() error = %v, want missing FOUNDRY_ENDPOINT", err)
	}
}

// TestFoundryAnthropicAPIKeySendsApiKeyHeader verifies the API-key path: when
// FOUNDRY_API_KEY is set, requests carry the x-api-key header and hit the
// Foundry Anthropic Messages path.
func TestFoundryAnthropicAPIKeySendsApiKeyHeader(t *testing.T) {
	t.Setenv("FOUNDRY_API_KEY", "test-key")

	type captured struct {
		apiKey string
		auth   string
		path   string
	}
	reqs := make(chan captured, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		reqs <- captured{
			apiKey: r.Header.Get("X-Api-Key"),
			auth:   r.Header.Get("Authorization"),
			path:   r.URL.Path,
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, foundryAnthropicMessageResponse)
	}))
	t.Cleanup(server.Close)

	model, err := NewFoundryAnthropicModelWithLogger(context.Background(), &AnthropicConfig{}, server.URL, "claude-haiku-4-5", nil, logr.Discard())
	if err != nil {
		t.Fatalf("NewFoundryAnthropicModelWithLogger() error = %v", err)
	}
	if model.Config.Model != "claude-haiku-4-5" {
		t.Fatalf("Config.Model = %q, want deployment name", model.Config.Model)
	}

	_, err = model.Client.Messages.New(context.Background(), anthropic.MessageNewParams{
		Model:     anthropic.Model("claude-haiku-4-5"),
		MaxTokens: 16,
		Messages:  []anthropic.MessageParam{anthropic.NewUserMessage(anthropic.NewTextBlock("hello"))},
	})
	if err != nil {
		t.Fatalf("messages error = %v", err)
	}

	got := <-reqs
	if got.apiKey != "test-key" {
		t.Fatalf("X-Api-Key = %q, want test-key", got.apiKey)
	}
	if got.auth != "" {
		t.Fatalf("Authorization = %q, want empty", got.auth)
	}
	if got.path != "/anthropic/v1/messages" {
		t.Fatalf("path = %q, want /anthropic/v1/messages", got.path)
	}
}

type fakeFoundryAnthropicCredential struct {
	t     *testing.T
	token string
}

func (c *fakeFoundryAnthropicCredential) GetToken(_ context.Context, opts policy.TokenRequestOptions) (azcore.AccessToken, error) {
	c.t.Helper()
	if len(opts.Scopes) != 1 || opts.Scopes[0] != azureai.AIFoundryScope {
		c.t.Fatalf("Scopes = %v, want AI Foundry scope", opts.Scopes)
	}
	return azcore.AccessToken{Token: c.token}, nil
}

// TestFoundryAnthropicWorkloadIdentityEagerProbeFailsReadiness verifies that with
// no API key and a credential that cannot acquire a token, construction fails
// with an actionable error (failing readiness at startup).
func TestFoundryAnthropicWorkloadIdentityEagerProbeFailsReadiness(t *testing.T) {
	t.Setenv("FOUNDRY_API_KEY", "")

	cfg := &AnthropicConfig{}
	_, err := NewFoundryAnthropicModelWithLogger(context.Background(), cfg, "https://example.services.ai.azure.com/", "claude-haiku-4-5", erroringFoundryCredential{}, logr.Discard())
	if err == nil || !strings.Contains(err.Error(), "no Azure credential resolved") {
		t.Fatalf("NewFoundryAnthropicModelWithLogger() error = %v, want credential-not-resolved", err)
	}
}

// TestFoundryAnthropicWorkloadIdentityEagerProbeSucceeds verifies the model is
// created when the credential can acquire a token at the AI Foundry scope.
func TestFoundryAnthropicWorkloadIdentityEagerProbeSucceeds(t *testing.T) {
	t.Setenv("FOUNDRY_API_KEY", "")

	cfg := &AnthropicConfig{}
	model, err := NewFoundryAnthropicModelWithLogger(context.Background(), cfg, "https://example.services.ai.azure.com/", "claude-haiku-4-5", &fakeFoundryAnthropicCredential{t: t, token: "entra-token"}, logr.Discard())
	if err != nil {
		t.Fatalf("NewFoundryAnthropicModelWithLogger() error = %v", err)
	}
	if model == nil {
		t.Fatalf("expected a Foundry Anthropic model")
	}
}

// TestFoundryAnthropicPassthroughInjectsBearerToken verifies that with
// APIKeyPassthrough enabled, the placeholder x-api-key is overwritten per request
// by the token carried in the context.
func TestFoundryAnthropicPassthroughInjectsBearerToken(t *testing.T) {
	t.Setenv("FOUNDRY_API_KEY", "")

	reqs := make(chan string, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		reqs <- r.Header.Get("X-Api-Key")
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, foundryAnthropicMessageResponse)
	}))
	t.Cleanup(server.Close)

	cfg := &AnthropicConfig{}
	cfg.APIKeyPassthrough = true

	model, err := NewFoundryAnthropicModelWithLogger(context.Background(), cfg, server.URL, "claude-haiku-4-5", nil, logr.Discard())
	if err != nil {
		t.Fatalf("NewFoundryAnthropicModelWithLogger() error = %v", err)
	}

	ctx := context.WithValue(context.Background(), BearerTokenKey, "caller-token")
	_, err = model.Client.Messages.New(ctx, anthropic.MessageNewParams{
		Model:     anthropic.Model("claude-haiku-4-5"),
		MaxTokens: 16,
		Messages:  []anthropic.MessageParam{anthropic.NewUserMessage(anthropic.NewTextBlock("hello"))},
	}, anthropicPassthroughOpts(ctx, model.Config)...)
	if err != nil {
		t.Fatalf("messages error = %v", err)
	}

	if got := <-reqs; got != "caller-token" {
		t.Fatalf("X-Api-Key = %q, want caller-token", got)
	}
}

// TestFoundryAnthropicPassthroughSkipsCredentialProbe verifies that enabling
// passthrough bypasses the Workload Identity eager token probe.
func TestFoundryAnthropicPassthroughSkipsCredentialProbe(t *testing.T) {
	t.Setenv("FOUNDRY_API_KEY", "")

	cfg := &AnthropicConfig{}
	cfg.APIKeyPassthrough = true

	model, err := NewFoundryAnthropicModelWithLogger(context.Background(), cfg, "https://example.services.ai.azure.com/", "claude-haiku-4-5", erroringFoundryCredential{}, logr.Discard())
	if err != nil {
		t.Fatalf("NewFoundryAnthropicModelWithLogger() error = %v, want success (passthrough must not probe the credential)", err)
	}
	if model == nil {
		t.Fatalf("expected a Foundry Anthropic model")
	}
}
