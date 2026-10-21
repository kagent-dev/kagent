package agent

import (
	"testing"

	configv1 "github.com/kagent-dev/kagent/go/api/gen/kagent/config/v1"
)

func TestGenerateContentConfig(t *testing.T) {
	tests := []struct {
		name  string
		model *configv1.ADKModelConfig
		want  int32 // 0 means expect nil config
	}{
		{name: "gemini with max output tokens", model: &configv1.ADKModelConfig{Provider: &configv1.ADKModelConfig_Gemini{Gemini: &configv1.ADKGemini{MaxOutputTokens: new(int64(1024))}}}, want: 1024},
		{name: "vertex with max output tokens", model: &configv1.ADKModelConfig{Provider: &configv1.ADKModelConfig_GeminiVertexAi{GeminiVertexAi: &configv1.ADKGeminiVertexAI{MaxOutputTokens: new(int64(512))}}}, want: 512},
		{name: "gemini unset", model: &configv1.ADKModelConfig{Provider: &configv1.ADKModelConfig_Gemini{Gemini: &configv1.ADKGemini{}}}, want: 0},
		{name: "vertex unset", model: &configv1.ADKModelConfig{Provider: &configv1.ADKModelConfig_GeminiVertexAi{GeminiVertexAi: &configv1.ADKGeminiVertexAI{}}}, want: 0},
		{name: "gemini zero", model: &configv1.ADKModelConfig{Provider: &configv1.ADKModelConfig_Gemini{Gemini: &configv1.ADKGemini{MaxOutputTokens: new(int64(0))}}}, want: 0},
		{name: "gemini negative", model: &configv1.ADKModelConfig{Provider: &configv1.ADKModelConfig_Gemini{Gemini: &configv1.ADKGemini{MaxOutputTokens: new(int64(-1))}}}, want: 0},
		{name: "non-gemini model", model: &configv1.ADKModelConfig{Provider: &configv1.ADKModelConfig_Openai{Openai: &configv1.ADKOpenAI{}}}, want: 0},
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
