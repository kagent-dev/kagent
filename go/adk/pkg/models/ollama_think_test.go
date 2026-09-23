package models

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/ollama/ollama/api"
	"google.golang.org/adk/v2/model"
	"google.golang.org/genai"
)

// TestGenerateContentSendsThink verifies the configured think switch reaches the
// chat request on both the streaming and the non-streaming path, and that an
// unset switch sends no think field, so Ollama keeps the model's default.
func TestGenerateContentSendsThink(t *testing.T) {
	off, on := false, true
	tests := []struct {
		name      string
		think     *bool
		wantThink any // the decoded JSON value; nil when the field must be absent
	}{
		{name: "unset sends no think field", think: nil, wantThink: nil},
		{name: "false turns thinking off", think: &off, wantThink: false},
		{name: "true turns thinking on", think: &on, wantThink: true},
	}

	for _, tt := range tests {
		for _, stream := range []bool{true, false} {
			name := tt.name + "/non-streaming"
			if stream {
				name = tt.name + "/streaming"
			}
			t.Run(name, func(t *testing.T) {
				var body map[string]any
				srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					raw, err := io.ReadAll(r.Body)
					if err != nil {
						t.Errorf("read request body: %v", err)
					}
					if err := json.Unmarshal(raw, &body); err != nil {
						t.Errorf("decode request body: %v", err)
					}
					w.Header().Set("Content-Type", "application/x-ndjson")
					_, _ = w.Write([]byte(`{"model":"qwen3.5:2b","message":{"role":"assistant","content":"hi"},"done":true,"done_reason":"stop"}` + "\n"))
				}))
				defer srv.Close()

				baseURL, err := url.Parse(srv.URL)
				if err != nil {
					t.Fatalf("parse url: %v", err)
				}
				m := &OllamaModel{
					Config: &OllamaConfig{Model: "qwen3.5:2b", Think: tt.think},
					Client: api.NewClient(baseURL, http.DefaultClient),
				}
				req := &model.LLMRequest{
					Contents: []*genai.Content{{Role: "user", Parts: []*genai.Part{{Text: "hello"}}}},
				}

				var text string
				for resp, err := range m.GenerateContent(context.Background(), req, stream) {
					if err != nil {
						t.Fatalf("unexpected error: %v", err)
					}
					if resp.ErrorMessage != "" {
						t.Fatalf("unexpected response error: %s", resp.ErrorMessage)
					}
					if resp.TurnComplete && resp.Content != nil && len(resp.Content.Parts) > 0 {
						text = resp.Content.Parts[0].Text
					}
				}
				if text != "hi" {
					t.Errorf("final text = %q, want %q", text, "hi")
				}

				got, present := body["think"]
				if tt.wantThink == nil {
					if present {
						t.Errorf("request carries think = %v, want no think field", got)
					}
					return
				}
				if got != tt.wantThink {
					t.Errorf("request think = %v (present %v), want %v", got, present, tt.wantThink)
				}
				if body["stream"] != stream {
					t.Errorf("request stream = %v, want %v", body["stream"], stream)
				}
			})
		}
	}
}
