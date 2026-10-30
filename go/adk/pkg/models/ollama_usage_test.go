package models

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/ollama/ollama/api"
	"google.golang.org/adk/v2/model"
	"google.golang.org/genai"
)

// TestGenerateUsageMetadataCachedTokens verifies that prompt_eval_cached_count
// from the final Ollama response is carried into CachedContentTokenCount, and
// that it stays zero when the server does not send the field (older Ollama).
func TestGenerateUsageMetadataCachedTokens(t *testing.T) {
	const withCached = `{"model":"qwen3.6","message":{"role":"assistant","content":"ok"},"done":true,"done_reason":"stop","prompt_eval_count":1200,"prompt_eval_cached_count":1024,"eval_count":30}`
	const withoutCached = `{"model":"qwen3.6","message":{"role":"assistant","content":"ok"},"done":true,"done_reason":"stop","prompt_eval_count":1200,"eval_count":30}`

	tests := []struct {
		name       string
		stream     bool
		final      string
		wantCached int32
	}{
		{name: "streaming with cached count", stream: true, final: withCached, wantCached: 1024},
		{name: "streaming without cached count", stream: true, final: withoutCached, wantCached: 0},
		{name: "non-streaming with cached count", stream: false, final: withCached, wantCached: 1024},
		{name: "non-streaming without cached count", stream: false, final: withoutCached, wantCached: 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/x-ndjson")
				_, _ = w.Write([]byte(tt.final + "\n"))
			}))
			defer srv.Close()

			baseURL, err := url.Parse(srv.URL)
			if err != nil {
				t.Fatalf("parse url: %v", err)
			}

			m := &OllamaModel{
				Config: &OllamaConfig{Model: "qwen3.6"},
				Client: api.NewClient(baseURL, http.DefaultClient),
			}

			req := &model.LLMRequest{
				Contents: []*genai.Content{
					{Role: "user", Parts: []*genai.Part{{Text: "hello"}}},
				},
			}

			var usage *genai.GenerateContentResponseUsageMetadata
			for resp, err := range m.GenerateContent(context.Background(), req, tt.stream) {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				if resp.ErrorMessage != "" {
					t.Fatalf("unexpected response error: %s", resp.ErrorMessage)
				}
				if resp.UsageMetadata != nil {
					usage = resp.UsageMetadata
				}
			}

			if usage == nil {
				t.Fatal("expected usage metadata, got nil")
			}
			if usage.PromptTokenCount != 1200 {
				t.Errorf("expected PromptTokenCount 1200, got %d", usage.PromptTokenCount)
			}
			if usage.CachedContentTokenCount != tt.wantCached {
				t.Errorf("expected CachedContentTokenCount %d, got %d", tt.wantCached, usage.CachedContentTokenCount)
			}
		})
	}
}
