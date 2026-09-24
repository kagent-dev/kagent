package memory

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/kagent-dev/kagent/go/adk/pkg/models"
	"github.com/kagent-dev/kagent/go/api/adk"
	apiv1alpha1 "github.com/kagent-dev/kagent/go/api/gen/kagent/api/v1alpha1"
	adksession "google.golang.org/adk/v2/session"

	"github.com/stretchr/testify/require"
)

type stubExchangedTokens struct{ token string }

func (s stubExchangedTokens) ExchangedToken(context.Context) (string, bool) {
	return s.token, s.token != ""
}

// Embedding calls run under the caller's request, so with API-key passthrough on
// they must carry the delegated identity like every other outbound call. New is
// the only path production takes, and it builds the embedding client itself, so
// the provider has to reach the wire through this config.
func TestNewSendsTheExchangedTokenOnEmbeddingCalls(t *testing.T) {
	for _, tt := range []struct {
		name      string
		provider  models.ExchangedTokenProvider
		wantToken string
	}{
		{name: "exchanged token wins", provider: stubExchangedTokens{token: "EXCHANGED"}, wantToken: "EXCHANGED"},
		{name: "no STS leaves the caller's token", wantToken: "CALLER"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			var gotAuth string
			embeddingServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				gotAuth = r.Header.Get("Authorization")
				vector := make([]float64, 768)
				vector[0] = 1
				w.Header().Set("Content-Type", "application/json")
				require.NoError(t, json.NewEncoder(w).Encode(map[string]any{
					"data":  []map[string]any{{"embedding": vector, "index": 0}},
					"model": "test-model",
				}))
			}))
			defer embeddingServer.Close()

			controllerClient := newMemoryControllerClient(t, &memoryTestServer{
				add: func(context.Context, *apiv1alpha1.MemoryServiceAddSessionRequest) (*apiv1alpha1.MemoryServiceAddSessionResponse, error) {
					return &apiv1alpha1.MemoryServiceAddSessionResponse{Id: "memory-1"}, nil
				},
			})

			service, err := New(Config{
				AgentName:        "test-agent",
				ControllerClient: controllerClient,
				EmbeddingConfig: &adk.EmbeddingConfig{
					Provider:          "openai",
					Model:             "test-model",
					BaseUrl:           embeddingServer.URL + "/v1",
					APIKeyPassthrough: true,
				},
				ExchangedTokens: tt.provider,
			})
			require.NoError(t, err)

			ctx := context.WithValue(t.Context(), models.BearerTokenKey, "CALLER")
			session := newMockSession("session", "user", []*adksession.Event{newMockEvent("user", "remember this")})
			require.NoError(t, service.AddSessionToMemory(ctx, session))

			require.Equal(t, "Bearer "+tt.wantToken, gotAuth)
		})
	}
}
