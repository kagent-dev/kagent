package azureai

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
	"github.com/openai/openai-go/v3"
)

type fakeCredential struct {
	t         *testing.T
	token     string
	err       error
	wantScope string
}

func (c fakeCredential) GetToken(_ context.Context, opts policy.TokenRequestOptions) (azcore.AccessToken, error) {
	if c.err != nil {
		return azcore.AccessToken{}, c.err
	}
	if c.t != nil {
		want := c.wantScope
		if want == "" {
			want = CognitiveServicesScope
		}
		if len(opts.Scopes) != 1 || opts.Scopes[0] != want {
			c.t.Fatalf("Scopes = %v, want %q", opts.Scopes, want)
		}
	}
	return azcore.AccessToken{Token: c.token}, nil
}

func TestAcquireTokenReturnsToken(t *testing.T) {
	got, err := AcquireToken(context.Background(), fakeCredential{t: t, token: "tok"}, "")
	if err != nil {
		t.Fatalf("AcquireToken() error = %v", err)
	}
	if got != "tok" {
		t.Fatalf("AcquireToken() = %q, want tok", got)
	}
}

func TestAcquireTokenUsesProvidedScope(t *testing.T) {
	got, err := AcquireToken(context.Background(), fakeCredential{t: t, token: "tok", wantScope: AIFoundryScope}, AIFoundryScope)
	if err != nil {
		t.Fatalf("AcquireToken() error = %v", err)
	}
	if got != "tok" {
		t.Fatalf("AcquireToken() = %q, want tok", got)
	}
}

func TestAcquireTokenPropagatesError(t *testing.T) {
	if _, err := AcquireToken(context.Background(), fakeCredential{err: fmt.Errorf("boom")}, ""); err == nil {
		t.Fatal("AcquireToken() error = nil, want error")
	}
}

func TestBearerTokenMiddlewareSetsAuthorization(t *testing.T) {
	mw := BearerTokenMiddleware(fakeCredential{t: t, token: "entra-token"}, "")
	req := httptest.NewRequest(http.MethodPost, "https://example.com/openai/deployments/x/embeddings", nil)
	_, err := mw(req, func(r *http.Request) (*http.Response, error) {
		if got := r.Header.Get("Authorization"); got != "Bearer entra-token" {
			t.Fatalf("Authorization = %q, want bearer token", got)
		}
		return &http.Response{StatusCode: http.StatusOK, Body: http.NoBody}, nil
	})
	if err != nil {
		t.Fatalf("middleware error = %v", err)
	}
}

func TestNewOpenAIClientValidates(t *testing.T) {
	if _, err := NewOpenAIClient(ClientConfig{Deployment: "d", APIKey: "k"}); err == nil {
		t.Fatal("want error for missing endpoint")
	}
	if _, err := NewOpenAIClient(ClientConfig{Endpoint: "https://e", APIKey: "k"}); err == nil {
		t.Fatal("want error for missing deployment")
	}
	if _, err := NewOpenAIClient(ClientConfig{Endpoint: "https://e", Deployment: "d"}); err == nil {
		t.Fatal("want error for missing auth")
	}
}

func TestNewOpenAIClientAPIKey(t *testing.T) {
	// A stray OPENAI_API_KEY must not leak to the Azure endpoint on the Api-Key
	// path (openai-go would otherwise send it as Authorization: Bearer).
	t.Setenv("OPENAI_API_KEY", "leak-me")

	var gotPath, gotAPIVersion, gotAPIKey, gotAuth string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotAPIVersion = r.URL.Query().Get("api-version")
		gotAPIKey = r.Header.Get("Api-Key")
		gotAuth = r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"object":"list","data":[],"model":"m","usage":{"prompt_tokens":0,"total_tokens":0}}`)
	}))
	defer server.Close()

	client, err := NewOpenAIClient(ClientConfig{
		Endpoint:   server.URL,
		Deployment: "dep",
		APIVersion: "2024-10-21",
		APIKey:     "secret",
	})
	if err != nil {
		t.Fatalf("NewOpenAIClient() error = %v", err)
	}
	_, _ = client.Embeddings.New(context.Background(), openai.EmbeddingNewParams{
		Model: openai.EmbeddingModel("dep"),
		Input: openai.EmbeddingNewParamsInputUnion{OfArrayOfStrings: []string{"x"}},
	})
	if gotPath != "/openai/deployments/dep/embeddings" {
		t.Fatalf("path = %q, want deployment embeddings path", gotPath)
	}
	if gotAPIVersion != "2024-10-21" {
		t.Fatalf("api-version = %q", gotAPIVersion)
	}
	if gotAPIKey != "secret" {
		t.Fatalf("Api-Key = %q", gotAPIKey)
	}
	if gotAuth != "" {
		t.Fatalf("Authorization = %q, want empty", gotAuth)
	}
}

func TestNewOpenAIClientWorkloadIdentity(t *testing.T) {
	var gotAuth, gotAPIKey string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		gotAPIKey = r.Header.Get("Api-Key")
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"object":"list","data":[],"model":"m","usage":{"prompt_tokens":0,"total_tokens":0}}`)
	}))
	defer server.Close()

	client, err := NewOpenAIClient(ClientConfig{
		Endpoint:   server.URL,
		Deployment: "dep",
		APIVersion: "2024-10-21",
		Credential: fakeCredential{token: "entra-token"},
	})
	if err != nil {
		t.Fatalf("NewOpenAIClient() error = %v", err)
	}
	_, _ = client.Embeddings.New(context.Background(), openai.EmbeddingNewParams{
		Model: openai.EmbeddingModel("dep"),
		Input: openai.EmbeddingNewParamsInputUnion{OfArrayOfStrings: []string{"x"}},
	})
	if gotAuth != "Bearer entra-token" {
		t.Fatalf("Authorization = %q, want bearer token", gotAuth)
	}
	if gotAPIKey != "" {
		t.Fatalf("Api-Key = %q, want empty", gotAPIKey)
	}
}

func TestNewAnthropicClientValidates(t *testing.T) {
	if _, err := NewAnthropicClient(AnthropicClientConfig{APIKey: "k"}); err == nil {
		t.Fatal("want error for missing endpoint")
	}
	if _, err := NewAnthropicClient(AnthropicClientConfig{Endpoint: "https://e"}); err == nil {
		t.Fatal("want error for missing auth")
	}
}

func TestNewAnthropicClientAPIKey(t *testing.T) {
	var gotPath, gotAPIKey, gotAuth string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotAPIKey = r.Header.Get("X-Api-Key")
		gotAuth = r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"id":"msg_1","type":"message","role":"assistant","model":"m","content":[{"type":"text","text":"hi"}],"stop_reason":"end_turn","usage":{"input_tokens":1,"output_tokens":1}}`)
	}))
	defer server.Close()

	client, err := NewAnthropicClient(AnthropicClientConfig{
		Endpoint: server.URL,
		APIKey:   "secret",
	})
	if err != nil {
		t.Fatalf("NewAnthropicClient() error = %v", err)
	}
	_, _ = client.Messages.New(context.Background(), anthropic.MessageNewParams{
		Model:     anthropic.Model("dep"),
		MaxTokens: 16,
		Messages: []anthropic.MessageParam{
			anthropic.NewUserMessage(anthropic.NewTextBlock("hi")),
		},
	})
	if gotPath != "/anthropic/v1/messages" {
		t.Fatalf("path = %q, want /anthropic/v1/messages", gotPath)
	}
	if gotAPIKey != "secret" {
		t.Fatalf("X-Api-Key = %q, want secret", gotAPIKey)
	}
	if gotAuth != "" {
		t.Fatalf("Authorization = %q, want empty", gotAuth)
	}
}

func TestNewAnthropicClientWorkloadIdentity(t *testing.T) {
	var gotAuth, gotAPIKey string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		gotAPIKey = r.Header.Get("X-Api-Key")
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"id":"msg_1","type":"message","role":"assistant","model":"m","content":[{"type":"text","text":"hi"}],"stop_reason":"end_turn","usage":{"input_tokens":1,"output_tokens":1}}`)
	}))
	defer server.Close()

	client, err := NewAnthropicClient(AnthropicClientConfig{
		Endpoint:   server.URL,
		Credential: fakeCredential{t: t, token: "entra-token", wantScope: AIFoundryScope},
	})
	if err != nil {
		t.Fatalf("NewAnthropicClient() error = %v", err)
	}
	_, _ = client.Messages.New(context.Background(), anthropic.MessageNewParams{
		Model:     anthropic.Model("dep"),
		MaxTokens: 16,
		Messages: []anthropic.MessageParam{
			anthropic.NewUserMessage(anthropic.NewTextBlock("hi")),
		},
	})
	if gotAuth != "Bearer entra-token" {
		t.Fatalf("Authorization = %q, want bearer token", gotAuth)
	}
	if gotAPIKey != "" {
		t.Fatalf("X-Api-Key = %q, want empty", gotAPIKey)
	}
}

func TestResolveFoundryUsesProvidedValues(t *testing.T) {
	t.Setenv(FoundryEndpointEnvVar, "env-endpoint")
	t.Setenv(FoundryDeploymentEnvVar, "env-deployment")
	t.Setenv(FoundryAPIVersionEnvVar, "env-version")

	ep, dep, ver := ResolveFoundry("cfg-endpoint", "cfg-deployment", "cfg-version")
	if ep != "cfg-endpoint" || dep != "cfg-deployment" || ver != "cfg-version" {
		t.Fatalf("ResolveFoundry() = (%q, %q, %q), want config values", ep, dep, ver)
	}
}

func TestResolveFoundryFallsBackToEnv(t *testing.T) {
	t.Setenv(FoundryEndpointEnvVar, "env-endpoint")
	t.Setenv(FoundryDeploymentEnvVar, "env-deployment")
	t.Setenv(FoundryAPIVersionEnvVar, "env-version")

	ep, dep, ver := ResolveFoundry("", "", "")
	if ep != "env-endpoint" || dep != "env-deployment" || ver != "env-version" {
		t.Fatalf("ResolveFoundry() = (%q, %q, %q), want env values", ep, dep, ver)
	}
}

func TestResolveFoundryDefaultsAPIVersion(t *testing.T) {
	t.Setenv(FoundryEndpointEnvVar, "")
	t.Setenv(FoundryDeploymentEnvVar, "")
	t.Setenv(FoundryAPIVersionEnvVar, "")

	ep, dep, ver := ResolveFoundry("e", "d", "")
	if ep != "e" || dep != "d" || ver != FoundryDefaultAPIVersion {
		t.Fatalf("ResolveFoundry() = (%q, %q, %q), want default api-version", ep, dep, ver)
	}
}

func TestApplyImplicitAuthUsesAPIKey(t *testing.T) {
	cfg := &ClientConfig{}
	if err := ApplyImplicitAuth(context.Background(), cfg, AuthOptions{
		APIKey:     "secret",
		Credential: fakeCredential{err: fmt.Errorf("should not be consulted")},
		EagerProbe: true,
	}); err != nil {
		t.Fatalf("ApplyImplicitAuth() error = %v", err)
	}
	if cfg.APIKey != "secret" || cfg.Credential != nil {
		t.Fatalf("APIKey=%q Credential=%v, want key set and no credential", cfg.APIKey, cfg.Credential)
	}
}

func TestApplyImplicitAuthUsesCredentialWithEagerProbe(t *testing.T) {
	cfg := &ClientConfig{}
	if err := ApplyImplicitAuth(context.Background(), cfg, AuthOptions{
		Credential: fakeCredential{t: t, token: "tok"},
		EagerProbe: true,
	}); err != nil {
		t.Fatalf("ApplyImplicitAuth() error = %v", err)
	}
	if cfg.APIKey != "" || cfg.Credential == nil {
		t.Fatalf("APIKey=%q Credential=%v, want credential set and no key", cfg.APIKey, cfg.Credential)
	}
}

func TestApplyImplicitAuthEagerProbeFailure(t *testing.T) {
	cfg := &ClientConfig{}
	err := ApplyImplicitAuth(context.Background(), cfg, AuthOptions{
		Credential: fakeCredential{err: fmt.Errorf("no ambient credential")},
		EagerProbe: true,
	})
	if err == nil || !strings.Contains(err.Error(), "no Azure credential resolved") {
		t.Fatalf("ApplyImplicitAuth() error = %v, want credential-not-resolved", err)
	}
}

func TestApplyImplicitAuthNoProbeSkipsTokenAcquisition(t *testing.T) {
	cfg := &ClientConfig{}
	// EagerProbe is false, so an erroring credential must not be consulted at
	// apply time (embeddings defer token acquisition to the first request).
	if err := ApplyImplicitAuth(context.Background(), cfg, AuthOptions{
		Credential: fakeCredential{err: fmt.Errorf("should not be consulted")},
		EagerProbe: false,
	}); err != nil {
		t.Fatalf("ApplyImplicitAuth() error = %v", err)
	}
	if cfg.Credential == nil {
		t.Fatalf("Credential not set")
	}
}

// TestResolveImplicitAuth covers the shared resolver both Azure surfaces use.
func TestResolveImplicitAuth(t *testing.T) {
	t.Run("api key short-circuits credential", func(t *testing.T) {
		apiKey, cred, err := ResolveImplicitAuth(context.Background(), AuthOptions{
			APIKey:     "secret",
			Credential: fakeCredential{err: fmt.Errorf("should not be consulted")},
			EagerProbe: true,
		})
		if err != nil {
			t.Fatalf("ResolveImplicitAuth() error = %v", err)
		}
		if apiKey != "secret" || cred != nil {
			t.Fatalf("apiKey=%q cred=%v, want key set and no credential", apiKey, cred)
		}
	})

	t.Run("credential path probes at scope", func(t *testing.T) {
		apiKey, cred, err := ResolveImplicitAuth(context.Background(), AuthOptions{
			Credential: fakeCredential{t: t, token: "tok", wantScope: AIFoundryScope},
			EagerProbe: true,
			EntraScope: AIFoundryScope,
		})
		if err != nil {
			t.Fatalf("ResolveImplicitAuth() error = %v", err)
		}
		if apiKey != "" || cred == nil {
			t.Fatalf("apiKey=%q cred=%v, want credential set and no key", apiKey, cred)
		}
	})

	t.Run("eager probe failure surfaces actionable error", func(t *testing.T) {
		_, _, err := ResolveImplicitAuth(context.Background(), AuthOptions{
			Credential: fakeCredential{err: fmt.Errorf("no ambient credential")},
			EagerProbe: true,
		})
		if err == nil || !strings.Contains(err.Error(), "no Azure credential resolved") {
			t.Fatalf("ResolveImplicitAuth() error = %v, want credential-not-resolved", err)
		}
	})
}
