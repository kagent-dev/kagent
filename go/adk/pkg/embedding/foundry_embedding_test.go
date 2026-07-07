package embedding

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore"
	"github.com/Azure/azure-sdk-for-go/sdk/azcore/policy"
	"github.com/kagent-dev/kagent/go/api/adk"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFoundryEmbeddingsURL(t *testing.T) {
	tests := []struct {
		name       string
		endpoint   string
		deployment string
		apiVersion string
		want       string
	}{
		{
			name:       "trailing slash",
			endpoint:   "https://acct.cognitiveservices.azure.com/",
			deployment: "text-embedding-3-small",
			apiVersion: "2024-10-21",
			want:       "https://acct.cognitiveservices.azure.com/openai/deployments/text-embedding-3-small/embeddings?api-version=2024-10-21",
		},
		{
			name:       "no trailing slash",
			endpoint:   "https://acct.cognitiveservices.azure.com",
			deployment: "emb",
			apiVersion: "2025-01-01",
			want:       "https://acct.cognitiveservices.azure.com/openai/deployments/emb/embeddings?api-version=2025-01-01",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := foundryEmbeddingsURL(tt.endpoint, tt.deployment, tt.apiVersion)
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestAdjustEmbeddingDimension(t *testing.T) {
	exact := make([]float32, TargetDimension)
	got, err := adjustEmbeddingDimension(exact)
	require.NoError(t, err)
	assert.Len(t, got, TargetDimension)

	longer := make([]float32, TargetDimension+16)
	for i := range longer {
		longer[i] = 1
	}
	got, err = adjustEmbeddingDimension(longer)
	require.NoError(t, err)
	assert.Len(t, got, TargetDimension)

	_, err = adjustEmbeddingDimension(make([]float32, TargetDimension-1))
	require.ErrorContains(t, err, "less than required")
}

func foundryEmbeddingResponseBody() []byte {
	vec := make([]float32, TargetDimension)
	for i := range vec {
		vec[i] = 0.01
	}
	body, _ := json.Marshal(foundryEmbeddingResponse{
		Data: []foundryEmbeddingData{{Embedding: vec, Index: 0}},
	})
	return body
}

// TestFoundryProviderAPIKey verifies the API-key auth path: the Api-Key header
// is set, the request targets the deployment embeddings path, and the response
// is returned as a 768-dim vector.
func TestFoundryProviderAPIKey(t *testing.T) {
	t.Setenv("FOUNDRY_API_KEY", "secret-key")

	var gotPath, gotAPIKey, gotAuth string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotAPIKey = r.Header.Get("Api-Key")
		gotAuth = r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(foundryEmbeddingResponseBody())
	}))
	defer server.Close()

	p := &foundryProvider{
		config: &adk.EmbeddingConfig{
			Provider:   "foundry",
			Endpoint:   server.URL,
			Deployment: "text-embedding-3-small",
			APIVersion: "2024-10-21",
		},
		httpClient: server.Client(),
	}

	embeddings, err := p.generate(context.Background(), []string{"hello"})
	require.NoError(t, err)
	require.Len(t, embeddings, 1)
	assert.Len(t, embeddings[0], TargetDimension)
	assert.Equal(t, "/openai/deployments/text-embedding-3-small/embeddings", gotPath)
	assert.Equal(t, "secret-key", gotAPIKey)
	assert.Empty(t, gotAuth)
}

type fakeEmbeddingCredential struct {
	token string
}

func (c *fakeEmbeddingCredential) GetToken(_ context.Context, _ policy.TokenRequestOptions) (azcore.AccessToken, error) {
	return azcore.AccessToken{Token: c.token}, nil
}

// TestFoundryProviderWorkloadIdentity verifies the implicit Workload Identity
// path: with no FOUNDRY_API_KEY, a bearer token from the credential is attached.
func TestFoundryProviderWorkloadIdentity(t *testing.T) {
	t.Setenv("FOUNDRY_API_KEY", "")

	var gotAuth, gotAPIKey string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		gotAPIKey = r.Header.Get("Api-Key")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(foundryEmbeddingResponseBody())
	}))
	defer server.Close()

	p := &foundryProvider{
		config: &adk.EmbeddingConfig{
			Provider:   "foundry",
			Endpoint:   server.URL,
			Deployment: "emb",
			APIVersion: "2024-10-21",
		},
		httpClient: server.Client(),
		credential: &fakeEmbeddingCredential{token: "entra-token"},
	}

	embeddings, err := p.generate(context.Background(), []string{"hello"})
	require.NoError(t, err)
	require.Len(t, embeddings, 1)
	assert.Equal(t, "Bearer entra-token", gotAuth)
	assert.Empty(t, gotAPIKey)
}
