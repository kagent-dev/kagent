package models

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"
	"github.com/go-logr/logr"
	"google.golang.org/adk/v2/model"
	"google.golang.org/genai"
)

// messageResponse is the anthropic Messages API payload served by the mock server.
const anthropicMessageResponse = `{
	"id":"msg_01","type":"message","role":"assistant","model":"claude-sonnet-4-20250514",
	"content":[{"type":"text","text":"pong"}],
	"stop_reason":"end_turn","stop_sequence":null,
	"usage":{"input_tokens":10,"output_tokens":5,"cache_read_input_tokens":8,"cache_creation_input_tokens":0}
}`

// TestAnthropicNonStreamingCachedTokens verifies CacheReadInputTokens flows into
// GenerateContentResponseUsageMetadata.CachedContentTokenCount for non-streaming calls.
func TestAnthropicNonStreamingCachedTokens(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, anthropicMessageResponse)
	}))
	defer srv.Close()

	client := anthropic.NewClient(
		option.WithAPIKey("test"),
		option.WithBaseURL(srv.URL),
	)
	m := &AnthropicModel{
		Config: &AnthropicConfig{Model: "claude-sonnet-4-20250514"},
		Client: client,
		Logger: logr.Discard(),
	}

	var got *model.LLMResponse
	for resp, err := range m.GenerateContent(context.Background(), &model.LLMRequest{
		Contents: []*genai.Content{{Role: "user", Parts: []*genai.Part{{Text: "ping"}}}},
	}, false) {
		if err != nil {
			t.Fatalf("GenerateContent error: %v", err)
		}
		got = resp
	}
	if got == nil || got.UsageMetadata == nil {
		t.Fatalf("usage metadata = %#v", got)
	}
	if got.UsageMetadata.PromptTokenCount != 10 {
		t.Fatalf("PromptTokenCount = %d, want 10", got.UsageMetadata.PromptTokenCount)
	}
	if got.UsageMetadata.CachedContentTokenCount != 8 {
		t.Fatalf("CachedContentTokenCount = %d, want 8", got.UsageMetadata.CachedContentTokenCount)
	}
}
