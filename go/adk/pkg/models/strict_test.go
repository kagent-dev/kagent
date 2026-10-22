package models

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/option"
	"github.com/stretchr/testify/require"
	"google.golang.org/adk/v2/model"
	"google.golang.org/genai"
)

func TestOpenAIModelStrictOutputSchema(t *testing.T) {
	tests := []struct {
		name      string
		apiFormat string
		reply     string
		format    func(body map[string]any) map[string]any
	}{
		{
			name:  "chat completions",
			reply: `{"id":"chatcmpl-1","object":"chat.completion","created":1,"model":"gpt-4o","choices":[{"index":0,"message":{"role":"assistant","content":"{}"},"finish_reason":"stop"}]}`,
			format: func(body map[string]any) map[string]any {
				return body["response_format"].(map[string]any)["json_schema"].(map[string]any)
			},
		},
		{
			name:      "responses",
			apiFormat: OpenAIAPIFormatResponses,
			reply: `{"id":"resp_1","object":"response","created_at":1,"status":"completed","model":"gpt-4o",
				"output":[{"type":"message","id":"msg_1","role":"assistant","status":"completed","content":[{"type":"output_text","text":"{}","annotations":[]}]}],
				"usage":{"input_tokens":1,"output_tokens":1,"total_tokens":2,"input_tokens_details":{"cached_tokens":0},"output_tokens_details":{"reasoning_tokens":0}}}`,
			format: func(body map[string]any) map[string]any {
				return body["text"].(map[string]any)["format"].(map[string]any)
			},
		},
	}
	strictCases := []struct {
		name   string
		strict *bool
	}{
		{name: "nil", strict: nil},
		{name: "false", strict: new(false)},
		{name: "true", strict: new(true)},
	}
	for _, tt := range tests {
		for _, sc := range strictCases {
			t.Run(tt.name+"/"+sc.name, func(t *testing.T) {
				var body map[string]any
				server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					encoded, _ := io.ReadAll(r.Body)
					_ = json.Unmarshal(encoded, &body)
					w.Header().Set("Content-Type", "application/json")
					_, _ = io.WriteString(w, tt.reply)
				}))
				defer server.Close()

				llm := &OpenAIModel{
					Config: &OpenAIConfig{Model: "gpt-4o", APIFormat: tt.apiFormat, StrictOutputSchema: sc.strict},
					Client: openai.NewClient(
						option.WithAPIKey("test"),
						option.WithBaseURL(server.URL),
						option.WithHTTPClient(server.Client()),
					),
					Logger: slog.New(slog.DiscardHandler),
				}
				request := &model.LLMRequest{
					Contents: []*genai.Content{{Role: "user", Parts: []*genai.Part{{Text: "ping"}}}},
					Config: &genai.GenerateContentConfig{ResponseJsonSchema: map[string]any{
						"type": "object", "properties": map[string]any{}, "additionalProperties": false,
					}},
				}
				for _, err := range llm.GenerateContent(context.Background(), request, false) {
					require.NoError(t, err)
				}

				strict, present := tt.format(body)["strict"]
				if sc.strict == nil {
					require.False(t, present, "strict must be omitted when nil")
					return
				}
				require.Equal(t, *sc.strict, strict)
			})
		}
	}
}
