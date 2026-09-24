package embedding

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/kagent-dev/kagent/go/adk/pkg/models"
	"github.com/kagent-dev/kagent/go/api/adk"
	"github.com/stretchr/testify/require"
)

type stubExchangedTokens struct{ token string }

func (s stubExchangedTokens) ExchangedToken(context.Context) (string, bool) {
	return s.token, s.token != ""
}

// New is the only way production builds an embedding client, so the provider has
// to survive that constructor. Nothing carries it per request, which makes a
// break here silent: embeddings would go out as the caller's own credential
// instead of the delegated one.
func TestNewCarriesTheExchangedTokenProviderToTheRequest(t *testing.T) {
	tests := []struct {
		name      string
		provider  models.ExchangedTokenProvider
		wantToken string
	}{
		{name: "exchanged token wins", provider: stubExchangedTokens{token: "EXCHANGED"}, wantToken: "EXCHANGED"},
		{name: "no provider leaves the caller's token", wantToken: "CALLER"},
		{name: "provider with nothing to offer", provider: stubExchangedTokens{}, wantToken: "CALLER"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var gotAuth string
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				gotAuth = r.Header.Get("Authorization")
				w.Header().Set("Content-Type", "application/json")
				require.NoError(t, json.NewEncoder(w).Encode(embeddingResponse("test-model")))
			}))
			defer server.Close()

			client, err := New(Config{
				EmbeddingConfig: &adk.EmbeddingConfig{
					Provider:          "openai",
					Model:             "test-model",
					BaseUrl:           server.URL + "/v1",
					APIKeyPassthrough: true,
				},
				ExchangedTokens: tt.provider,
			})
			require.NoError(t, err)

			ctx := context.WithValue(context.Background(), models.BearerTokenKey, "CALLER")
			_, err = client.Generate(ctx, []string{"hello"})
			require.NoError(t, err)

			require.Equal(t, "Bearer "+tt.wantToken, gotAuth)
		})
	}
}
