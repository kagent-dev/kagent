package models

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"google.golang.org/adk/v2/model"
	"google.golang.org/genai"
)

func TestGeminiCustomEndpoints(t *testing.T) {
	t.Setenv("GOOGLE_API_KEY", "")
	t.Setenv("GEMINI_API_KEY", "")
	t.Setenv("GOOGLE_GENAI_USE_VERTEXAI", "true")
	for _, tc := range []struct{ model, encoded string }{
		{"catalog-alias", "catalog-alias"},
		{"team/gemini-listed", "team%2Fgemini-listed"},
		{"models/gemini-listed", "models%2Fgemini-listed"},
		{"tunedModels/custom", "tunedModels%2Fcustom"},
		{"team/model:variant", "team%2Fmodel%3Avariant"},
		{"team%2Fmodel", "team%252Fmodel"},
	} {
		for _, trailingSlash := range []string{"", "/"} {
			for _, stream := range []bool{false, true} {
				t.Run(tc.model+trailingSlash+map[bool]string{false: "unary", true: "stream"}[stream], func(t *testing.T) {
					modelID, prefix, path := tc.model, "/team/v1", "/models/"+tc.encoded+":generateContent"
					if stream {
						path = "/models/" + tc.encoded + ":streamGenerateContent?alt=sse"
					}
					headers := map[string]string{"x-route": "team"}
					if modelID == "team/gemini-listed" {
						headers["Authorization"] = "Bearer kagent-credential-injected"
					} else if trailingSlash != "" {
						headers["x-goog-api-key"] = "kagent-credential-injected"
					}
					requests := 0
					server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
						requests++
						require.Equal(t, http.MethodPost, r.Method)
						require.Equal(t, prefix+path, r.URL.RequestURI())
						require.Equal(t, "team", r.Header.Get("x-route"))
						require.Equal(t, headers["Authorization"], r.Header.Get("Authorization"))
						require.Equal(t, headers["x-goog-api-key"], r.Header.Get("x-goog-api-key"))
						var body map[string]json.RawMessage
						require.NoError(t, json.NewDecoder(r.Body).Decode(&body))
						require.Contains(t, body, "contents")
						payload := `{"candidates":[{"content":{"role":"model","parts":[{"text":"ok"}]},"finishReason":"STOP"}]}`
						if stream {
							w.Header().Set("Content-Type", "text/event-stream")
							_, _ = io.WriteString(w, "data: "+payload+"\n\n")
						} else {
							w.Header().Set("Content-Type", "application/json")
							_, _ = io.WriteString(w, payload)
						}
					}))
					t.Cleanup(server.Close)
					llm, err := NewGeminiModel(t.Context(), GeminiConfig{
						Model: modelID, BaseURL: server.URL + prefix + trailingSlash,
						TransportConfig: TransportConfig{Headers: headers, TLSInsecureSkipVerify: new(true)},
					})
					require.NoError(t, err)
					var text strings.Builder
					request := &model.LLMRequest{Model: modelID, Contents: genai.Text("hello")}
					for response, err := range llm.GenerateContent(t.Context(), request, stream) {
						require.NoError(t, err)
						if response.Content != nil {
							for _, part := range response.Content.Parts {
								text.WriteString(part.Text)
							}
						}
					}
					require.Contains(t, text.String(), "ok")
					require.Equal(t, 1, requests)
					require.Equal(t, modelID, request.Model)
					require.Equal(t, modelID, llm.Name())
				})
			}
		}
	}
}

func TestGeminiDirectRequiresAPIKey(t *testing.T) {
	_, err := NewGeminiModel(t.Context(), GeminiConfig{Model: "gemini-test"})
	require.ErrorContains(t, err, "requires GOOGLE_API_KEY")
}

func TestGeminiAuthenticationIndependentOfEndpoint(t *testing.T) {
	t.Setenv("GOOGLE_GENAI_USE_VERTEXAI", "false")
	originalTransport := http.DefaultTransport
	t.Cleanup(func() { http.DefaultTransport = originalTransport })
	for _, baseURL := range []string{"", "https://models.example.com/v1"} {
		for _, actorJWT := range []bool{false, true} {
			t.Run(baseURL+map[bool]string{false: "/key", true: "/actor"}[actorJWT], func(t *testing.T) {
				calls := 0
				http.DefaultTransport = geminiTestTransport(func(req *http.Request) (*http.Response, error) {
					calls++
					if actorJWT {
						require.Equal(t, "Bearer actor-placeholder", req.Header.Get("Authorization"))
						require.Empty(t, req.Header.Get("x-goog-api-key"))
					} else {
						require.Equal(t, "configured-key", req.Header.Get("x-goog-api-key"))
						require.Empty(t, req.Header.Get("Authorization"))
					}
					if baseURL == "" {
						require.Equal(t, "generativelanguage.googleapis.com", req.URL.Host)
						require.Equal(t, "/v1beta/models/gemini-test:generateContent", req.URL.Path)
					} else {
						require.Equal(t, "models.example.com", req.URL.Host)
						require.Equal(t, "/v1/models/gemini-test:generateContent", req.URL.Path)
					}
					return &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": {"application/json"}}, Body: io.NopCloser(strings.NewReader(`{"candidates":[{"content":{"role":"model","parts":[{"text":"ok"}]},"finishReason":"STOP"}]}`))}, nil
				})
				cfg := GeminiConfig{Model: "gemini-test", BaseURL: baseURL, APIKey: "configured-key"}
				if actorJWT {
					cfg.Headers = map[string]string{"Authorization": "Bearer actor-placeholder"}
				}
				llm, err := NewGeminiModel(t.Context(), cfg)
				require.NoError(t, err)
				for response, err := range llm.GenerateContent(t.Context(), &model.LLMRequest{Contents: genai.Text("hello")}, false) {
					require.NoError(t, err)
					require.Equal(t, "ok", response.Content.Parts[0].Text)
				}
				require.Equal(t, 1, calls)
			})
		}
	}
}

type geminiTestTransport func(*http.Request) (*http.Response, error)

func (f geminiTestTransport) RoundTrip(req *http.Request) (*http.Response, error) { return f(req) }
