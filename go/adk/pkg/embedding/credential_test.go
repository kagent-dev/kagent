package embedding

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/a2aproject/a2a-go/v2/a2asrv"
	"github.com/stretchr/testify/require"

	configv1 "github.com/kagent-dev/kagent/go/api/gen/kagent/config/v1"
)

func TestGeminiAndBedrockEmbeddingCredentials(t *testing.T) {
	for _, provider := range []string{"gemini", "bedrock"} {
		for _, passthrough := range []bool{false, true} {
			name := provider + "/gateway"
			if passthrough {
				name = provider + "/passthrough"
			}
			t.Run(name, func(t *testing.T) {
				header, prefix := "X-Goog-Api-Key", ""
				if provider == "bedrock" {
					header, prefix = "Authorization", "Bearer "
				}
				var received []string
				server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					received = append(received, r.Header.Get(header))
					require.Equal(t, "embedding", r.Header.Get("X-Client"))
					vector := make([]float32, TargetDimension)
					vector[0] = 1
					w.Header().Set("Content-Type", "application/json")
					if provider == "bedrock" {
						require.NoError(t, json.NewEncoder(w).Encode(bedrockEmbeddingResponse{Embedding: vector}))
					} else {
						require.NoError(t, json.NewEncoder(w).Encode(struct {
							Embeddings []struct {
								Values []float32 `json:"values"`
							} `json:"embeddings"`
						}{Embeddings: []struct {
							Values []float32 `json:"values"`
						}{{Values: vector}}}))
					}
				}))
				defer server.Close()
				// Compiled runtimes supply inert SDK keys; the transport or gateway
				// replaces them with the configured credential.
				t.Setenv("GOOGLE_API_KEY", "kagent-credential-injected")
				t.Setenv("GOOGLE_GEMINI_BASE_URL", server.URL)
				t.Setenv("AWS_BEARER_TOKEN_BEDROCK", "kagent-credential-injected")
				t.Setenv("AWS_ENDPOINT_URL_BEDROCK_RUNTIME", server.URL)
				t.Setenv("AWS_MAX_ATTEMPTS", "1")
				config := &configv1.ADKEmbeddingConfig{Provider: provider, Model: "embedding-model", TlsInsecureSkipVerify: new(true), Headers: map[string]string{"X-Client": "embedding"}}
				if passthrough {
					config.PassthroughHeaders = []*configv1.InvocationCredentialHeader{{Name: header, Prefix: prefix}}
				}
				client, err := New(Config{EmbeddingConfig: config})
				require.NoError(t, err)
				for _, token := range []string{"first-caller", "second-caller"} {
					ctx, _ := a2asrv.NewCallContext(t.Context(), a2asrv.NewServiceParams(map[string][]string{"Authorization": {"Bearer " + token}}))
					_, err := client.Generate(ctx, []string{"hello"})
					require.NoError(t, err)
					want := "kagent-credential-injected"
					if passthrough {
						want = token
					}
					require.Equal(t, prefix+want, received[len(received)-1])
				}
				if passthrough {
					_, err := client.Generate(t.Context(), []string{"missing caller"})
					require.ErrorContains(t, err, "credential unavailable")
					require.Len(t, received, 2)
				}
			})
		}
	}
}
