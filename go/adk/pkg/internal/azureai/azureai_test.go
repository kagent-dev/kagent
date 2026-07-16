package azureai

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore"
	"github.com/Azure/azure-sdk-for-go/sdk/azcore/policy"
	"github.com/openai/openai-go/v3"
)

type fakeCredential struct {
	t     *testing.T
	token string
	err   error
}

func (c fakeCredential) GetToken(_ context.Context, opts policy.TokenRequestOptions) (azcore.AccessToken, error) {
	if c.err != nil {
		return azcore.AccessToken{}, c.err
	}
	if c.t != nil && (len(opts.Scopes) != 1 || opts.Scopes[0] != CognitiveServicesScope) {
		c.t.Fatalf("Scopes = %v, want cognitive services scope", opts.Scopes)
	}
	return azcore.AccessToken{Token: c.token}, nil
}

func TestAcquireTokenReturnsToken(t *testing.T) {
	got, err := AcquireToken(context.Background(), fakeCredential{t: t, token: "tok"})
	if err != nil {
		t.Fatalf("AcquireToken() error = %v", err)
	}
	if got != "tok" {
		t.Fatalf("AcquireToken() = %q, want tok", got)
	}
}

func TestAcquireTokenPropagatesError(t *testing.T) {
	if _, err := AcquireToken(context.Background(), fakeCredential{err: fmt.Errorf("boom")}); err == nil {
		t.Fatal("AcquireToken() error = nil, want error")
	}
}

func TestBearerTokenMiddlewareSetsAuthorization(t *testing.T) {
	mw := BearerTokenMiddleware(fakeCredential{t: t, token: "entra-token"})
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
