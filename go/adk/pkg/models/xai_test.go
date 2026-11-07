package models

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/adk/v2/model"
	"google.golang.org/genai"
)

func TestNewXAIModel(t *testing.T) {
	t.Run("Given no XAI_API_KEY, when the model is created without passthrough, assert that creation fails", func(t *testing.T) {
		t.Setenv("XAI_API_KEY", "")
		t.Setenv("XAI_API_BASE", "")

		_, err := NewXAIModel(t.Context(), &XAIConfig{Model: "grok-4.7"})
		require.ErrorContains(t, err, "XAI_API_KEY")
	})

	scenarios := []struct {
		name        string
		envAPIKey   string
		envAPIBase  string
		config      *XAIConfig
		wantBaseURL string
	}{
		{
			name:        "Given an API key and no base URL, when the model is created, assert that it targets the xAI default",
			envAPIKey:   "test-key",
			config:      &XAIConfig{Model: "grok-4.7"},
			wantBaseURL: "https://api.x.ai/v1",
		},
		{
			name:        "Given XAI_API_BASE, when the model is created without a configured base URL, assert that it targets XAI_API_BASE",
			envAPIKey:   "test-key",
			envAPIBase:  "https://gateway.example.com/xai/v1",
			config:      &XAIConfig{Model: "grok-4.7"},
			wantBaseURL: "https://gateway.example.com/xai/v1",
		},
		{
			name:        "Given XAI_API_BASE, when the model is created with a configured base URL, assert that the configured URL wins",
			envAPIKey:   "test-key",
			envAPIBase:  "https://from-env.example.com/v1",
			config:      &XAIConfig{Model: "grok-4.7", BaseUrl: "https://from-config.example.com/v1"},
			wantBaseURL: "https://from-config.example.com/v1",
		},
		{
			name:        "Given no XAI_API_KEY, when the model is created with passthrough, assert that it targets the xAI default",
			config:      &XAIConfig{TransportConfig: TransportConfig{APIKeyPassthrough: true}, Model: "grok-4.7"},
			wantBaseURL: "https://api.x.ai/v1",
		},
	}
	for _, sc := range scenarios {
		t.Run(sc.name, func(t *testing.T) {
			t.Setenv("XAI_API_KEY", sc.envAPIKey)
			t.Setenv("XAI_API_BASE", sc.envAPIBase)

			m, err := NewXAIModel(t.Context(), sc.config)
			require.NoError(t, err)
			assert.Equal(t, sc.wantBaseURL, m.inner.Config.BaseUrl)
			assert.Equal(t, sc.config.Model, m.Name())
		})
	}
}

func TestXAIModelGenerateContent(t *testing.T) {
	t.Run("Given an xAI endpoint and a token limit, when a Grok model generates content, assert that it posts a chat completion with a Bearer key and max_completion_tokens", func(t *testing.T) {
		type captured struct {
			method, path, auth string
			body               map[string]any
		}
		reqs := make(chan captured, 1)
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			raw, _ := io.ReadAll(r.Body)
			var body map[string]any
			_ = json.Unmarshal(raw, &body)
			reqs <- captured{method: r.Method, path: r.URL.Path, auth: r.Header.Get("Authorization"), body: body}
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprint(w, `{"id":"chatcmpl-test","object":"chat.completion","created":0,"model":"grok-4.7","choices":[{"index":0,"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}]}`)
		}))
		t.Cleanup(server.Close)
		t.Setenv("XAI_API_KEY", "test-key")
		t.Setenv("XAI_API_BASE", server.URL+"/v1")

		llm, err := NewXAIModel(t.Context(), &XAIConfig{Model: "grok-4.7", MaxTokens: new(int64(512))})
		require.NoError(t, err)
		request := &model.LLMRequest{Contents: []*genai.Content{{Role: "user", Parts: []*genai.Part{{Text: "hello"}}}}}
		var text string
		for resp, err := range llm.GenerateContent(t.Context(), request, false) {
			require.NoError(t, err)
			if resp.Content != nil && len(resp.Content.Parts) > 0 {
				text += resp.Content.Parts[0].Text
			}
		}

		got := <-reqs
		assert.Equal(t, http.MethodPost, got.method)
		assert.Equal(t, "/v1/chat/completions", got.path)
		assert.Equal(t, "Bearer test-key", got.auth)
		assert.Equal(t, "grok-4.7", got.body["model"])
		assert.Equal(t, float64(512), got.body["max_completion_tokens"])
		assert.NotContains(t, got.body, "max_tokens", "xAI deprecates max_tokens on chat completions")
		assert.Equal(t, "ok", text)
	})
}
