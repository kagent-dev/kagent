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

// TestGenerateStreamingAggregatesToolCalls verifies that tool calls emitted by
// Ollama in an intermediate (done=false) chunk are not dropped when the final
// (done=true) chunk carries no tool calls. This mirrors real Ollama streaming
// behavior (e.g. glm-5.2) where tool_calls arrive before the terminating chunk.
func TestGenerateStreamingAggregatesToolCalls(t *testing.T) {
	// Stream: first an empty content chunk with the tool call, then a terminating
	// chunk with no tool calls.
	chunks := []string{
		`{"model":"glm-5.2","message":{"role":"assistant","content":"","tool_calls":[{"id":"call_1","function":{"index":0,"name":"show-weather-dashboard","arguments":{"location":"New York"}}}]},"done":false}`,
		`{"model":"glm-5.2","message":{"role":"assistant","content":""},"done":true,"done_reason":"stop","prompt_eval_count":10,"eval_count":5}`,
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/x-ndjson")
		flusher, _ := w.(http.Flusher)
		for _, c := range chunks {
			_, _ = w.Write([]byte(c + "\n"))
			if flusher != nil {
				flusher.Flush()
			}
		}
	}))
	defer srv.Close()

	baseURL, err := url.Parse(srv.URL)
	if err != nil {
		t.Fatalf("parse url: %v", err)
	}

	m := &OllamaModel{
		Config: &OllamaConfig{Model: "glm-5.2"},
		Client: api.NewClient(baseURL, http.DefaultClient),
	}

	req := &model.LLMRequest{
		Contents: []*genai.Content{
			{Role: "user", Parts: []*genai.Part{{Text: "show weather in NY"}}},
		},
	}

	var functionCalls []*genai.FunctionCall
	for resp, err := range m.GenerateContent(context.Background(), req, true) {
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if resp.ErrorMessage != "" {
			t.Fatalf("unexpected response error: %s", resp.ErrorMessage)
		}
		if resp.Content == nil {
			continue
		}
		for _, part := range resp.Content.Parts {
			if part.FunctionCall != nil {
				functionCalls = append(functionCalls, part.FunctionCall)
			}
		}
	}

	if len(functionCalls) != 1 {
		t.Fatalf("expected 1 function call, got %d", len(functionCalls))
	}
	if functionCalls[0].Name != "show-weather-dashboard" {
		t.Errorf("expected tool name show-weather-dashboard, got %q", functionCalls[0].Name)
	}
	if got := functionCalls[0].Args["location"]; got != "New York" {
		t.Errorf("expected location \"New York\", got %v", got)
	}
}
