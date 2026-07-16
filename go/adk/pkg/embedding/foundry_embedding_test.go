package embedding

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore"
	"github.com/Azure/azure-sdk-for-go/sdk/azcore/policy"
	"github.com/kagent-dev/kagent/go/api/adk"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func openAIEmbeddingResponseBody() string {
	vals := make([]string, TargetDimension)
	for i := range vals {
		vals[i] = "0.01"
	}
	return `{"object":"list","data":[{"object":"embedding","index":0,"embedding":[` + strings.Join(vals, ",") + `]}],"model":"text-embedding-3-small","usage":{"prompt_tokens":1,"total_tokens":1}}`
}

// TestFoundryProviderAPIKey verifies the API-key auth path: the Api-Key header
// is set, the request targets the deployment embeddings path with the api-version
// query, and the response is returned as a 768-dim vector.
func TestFoundryProviderAPIKey(t *testing.T) {
	t.Setenv("FOUNDRY_API_KEY", "secret-key")

	var gotPath, gotAPIVersion, gotAPIKey, gotAuth string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotAPIVersion = r.URL.Query().Get("api-version")
		gotAPIKey = r.Header.Get("Api-Key")
		gotAuth = r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(openAIEmbeddingResponseBody()))
	}))
	defer server.Close()

	p, err := newFoundryProvider(&adk.EmbeddingConfig{
		Provider:   "foundry",
		Model:      "text-embedding-3-small",
		Endpoint:   server.URL,
		Deployment: "text-embedding-3-small",
		APIVersion: "2024-10-21",
	}, nil)
	require.NoError(t, err)

	embeddings, err := p.generate(context.Background(), []string{"hello"})
	require.NoError(t, err)
	require.Len(t, embeddings, 1)
	assert.Len(t, embeddings[0], TargetDimension)
	assert.Equal(t, "/openai/deployments/text-embedding-3-small/embeddings", gotPath)
	assert.Equal(t, "2024-10-21", gotAPIVersion)
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
// path: with no FOUNDRY_API_KEY, a bearer token from the injected credential is
// attached and no Api-Key header is sent.
func TestFoundryProviderWorkloadIdentity(t *testing.T) {
	t.Setenv("FOUNDRY_API_KEY", "")

	var gotAuth, gotAPIKey string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		gotAPIKey = r.Header.Get("Api-Key")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(openAIEmbeddingResponseBody()))
	}))
	defer server.Close()

	p, err := newFoundryProvider(&adk.EmbeddingConfig{
		Provider:   "foundry",
		Model:      "emb",
		Endpoint:   server.URL,
		Deployment: "emb",
		APIVersion: "2024-10-21",
	}, &fakeEmbeddingCredential{token: "entra-token"})
	require.NoError(t, err)

	embeddings, err := p.generate(context.Background(), []string{"hello"})
	require.NoError(t, err)
	require.Len(t, embeddings, 1)
	assert.Equal(t, "Bearer entra-token", gotAuth)
	assert.Empty(t, gotAPIKey)
}
