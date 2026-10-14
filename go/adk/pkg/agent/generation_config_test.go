package agent

import (
	"testing"

	"github.com/kagent-dev/kagent/go/api/adk"
)

func TestGenerateContentConfig(t *testing.T) {
	tests := []struct {
		name  string
		model *adk.Model
		want  int32 // 0 means expect nil config
	}{
		{name: "gemini with max output tokens", model: &adk.Model{Provider: &adk.Model_Gemini{Gemini: &adk.Gemini{MaxOutputTokens: new(int64(1024))}}}, want: 1024},
		{name: "vertex with max output tokens", model: &adk.Model{Provider: &adk.Model_GeminiVertexAi{GeminiVertexAi: &adk.GeminiVertexAI{MaxOutputTokens: new(int64(512))}}}, want: 512},
		{name: "gemini unset", model: &adk.Model{Provider: &adk.Model_Gemini{Gemini: &adk.Gemini{}}}, want: 0},
		{name: "vertex unset", model: &adk.Model{Provider: &adk.Model_GeminiVertexAi{GeminiVertexAi: &adk.GeminiVertexAI{}}}, want: 0},
		{name: "gemini zero", model: &adk.Model{Provider: &adk.Model_Gemini{Gemini: &adk.Gemini{MaxOutputTokens: new(int64(0))}}}, want: 0},
		{name: "gemini negative", model: &adk.Model{Provider: &adk.Model_Gemini{Gemini: &adk.Gemini{MaxOutputTokens: new(int64(-1))}}}, want: 0},
		{name: "non-gemini model", model: &adk.Model{Provider: &adk.Model_Openai{Openai: &adk.OpenAI{}}}, want: 0},
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
