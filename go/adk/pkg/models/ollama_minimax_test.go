package models

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"google.golang.org/adk/v2/model"
	"google.golang.org/genai"
)

type ollamaMiniMaxTransport func(*http.Request) (*http.Response, error)

func (f ollamaMiniMaxTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	return f(r)
}

// Exercise the constructor and SDK serialization without contacting a provider.
func TestOllamaMiniMaxRequests(t *testing.T) {
	t.Setenv("OLLAMA_API_BASE", "")
	original := http.DefaultTransport
	t.Cleanup(func() { http.DefaultTransport = original })

	for _, tc := range []struct {
		name, model, host, key, wantHost string
	}{
		{"flash", "minimax-m3.1-flash", "", "placeholder", "api.ollama.com"},
		{"m3", "minimax-m3", "", "placeholder", "api.ollama.com"},
		{"saved legacy", "minimax-m2.7", "", "placeholder", "api.ollama.com"},
		{"cloud tag", "minimax-m3.1-flash:cloud", "", "placeholder", "api.ollama.com"},
		{"local daemon", "minimax-m3.1-flash", "", "", "localhost:11434"},
		{"custom host", "minimax-m3.1-flash", "https://models.example.test", "placeholder", "models.example.test"},
		{"custom model", "custom-model", "https://models.example.test", "", "models.example.test"},
	} {
		for _, stream := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/stream=%t", tc.name, stream), func(t *testing.T) {
				calls := 0
				http.DefaultTransport = ollamaMiniMaxTransport(func(r *http.Request) (*http.Response, error) {
					calls++
					require.Equal(t, tc.wantHost, r.URL.Host)
					require.Equal(t, "/api/chat", r.URL.Path)
					require.Equal(t, http.MethodPost, r.Method)
					wantAuth := ""
					if tc.wantHost == "api.ollama.com" {
						wantAuth = "Bearer placeholder"
					}
					require.Equal(t, wantAuth, r.Header.Get("Authorization"))
					var body map[string]json.RawMessage
					require.NoError(t, json.NewDecoder(r.Body).Decode(&body))
					require.JSONEq(t, fmt.Sprintf("%q", tc.model), string(body["model"]))
					require.JSONEq(t, fmt.Sprint(stream), string(body["stream"]))
					for _, field := range []string{"think", "thinking", "reasoning_effort", "service_tier"} {
						require.NotContains(t, body, field)
					}
					// Preserve the adapter's existing image placeholder behavior.
					require.Contains(t, string(body["messages"]), "this provider integration cannot pass images.")
					require.Contains(t, string(body["tools"]), `"name":"weather"`)
					return &http.Response{
						StatusCode: http.StatusOK,
						Header:     http.Header{"Content-Type": {"application/x-ndjson"}},
						Body:       io.NopCloser(strings.NewReader(`{"message":{"role":"assistant","content":"ok"},"done":true}` + "\n")),
						Request:    r,
					}, nil
				})
				m, err := NewOllamaModel(t.Context(), &OllamaConfig{Model: tc.model, Host: tc.host, APIKey: tc.key})
				require.NoError(t, err)
				request := &model.LLMRequest{
					Contents: []*genai.Content{{Role: "user", Parts: []*genai.Part{
						{Text: "Describe the weather"},
						{InlineData: &genai.Blob{MIMEType: "image/png", Data: []byte{1, 2, 3}}},
					}}},
					Config: &genai.GenerateContentConfig{Tools: []*genai.Tool{{FunctionDeclarations: []*genai.FunctionDeclaration{{Name: "weather"}}}}},
				}
				responses := 0
				for response, err := range m.GenerateContent(t.Context(), request, stream) {
					require.NoError(t, err)
					require.Empty(t, response.ErrorMessage)
					responses++
				}
				require.Positive(t, responses)
				require.Equal(t, 1, calls)
			})
		}
	}
}
