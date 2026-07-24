package agent

import (
	"testing"

	"github.com/kagent-dev/kagent/go/api/adk"
)

func TestGenerateContentConfig(t *testing.T) {
	tests := []struct {
		name  string
		model adk.Model
		want  int32 // 0 means expect nil config
	}{
		{name: "gemini with max output tokens", model: &adk.Gemini{MaxOutputTokens: new(1024)}, want: 1024},
		{name: "vertex with max output tokens", model: &adk.GeminiVertexAI{MaxOutputTokens: new(512)}, want: 512},
		{name: "gemini unset", model: &adk.Gemini{}, want: 0},
		{name: "vertex unset", model: &adk.GeminiVertexAI{}, want: 0},
		{name: "gemini zero", model: &adk.Gemini{MaxOutputTokens: new(0)}, want: 0},
		{name: "gemini negative", model: &adk.Gemini{MaxOutputTokens: new(-1)}, want: 0},
		{name: "non-gemini model", model: &adk.OpenAI{}, want: 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := generateContentConfig(tt.model)
			if tt.want == 0 {
				if got != nil {
					t.Errorf("generateContentConfig() = %+v, want nil", got)
				}
				return
			}
			if got == nil {
				t.Fatalf("generateContentConfig() = nil, want MaxOutputTokens=%d", tt.want)
			}
			if got.MaxOutputTokens != tt.want {
				t.Errorf("MaxOutputTokens = %d, want %d", got.MaxOutputTokens, tt.want)
			}
		})
	}
}
