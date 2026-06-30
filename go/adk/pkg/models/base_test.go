package models

import (
	"testing"

	"google.golang.org/genai"
)

func TestMergeSystemInstructionFromConfig(t *testing.T) {
	tests := []struct {
		name     string
		existing string
		config   *genai.GenerateContentConfig
		want     string
	}{
		{
			name:     "nil config returns trimmed existing",
			existing: "  hello  ",
			want:     "hello",
		},
		{
			name: "config only",
			config: &genai.GenerateContentConfig{
				SystemInstruction: &genai.Content{
					Parts: []*genai.Part{
						{Text: "You are helpful."},
						{Text: "Be concise."},
					},
				},
			},
			want: "You are helpful.\nBe concise.",
		},
		{
			name: "skips empty text parts",
			config: &genai.GenerateContentConfig{
				SystemInstruction: &genai.Content{
					Parts: []*genai.Part{
						{Text: "  one  "},
						{Text: ""},
						{Text: "two"},
					},
				},
			},
			want: "one  \ntwo",
		},
		{
			name:     "merges existing with config",
			existing: "From contents",
			config: &genai.GenerateContentConfig{
				SystemInstruction: &genai.Content{
					Parts: []*genai.Part{{Text: "From config"}},
				},
			},
			want: "From contents\nFrom config",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := mergeSystemInstructionFromConfig(tt.existing, tt.config)
			if got != tt.want {
				t.Errorf("mergeSystemInstructionFromConfig() = %q, want %q", got, tt.want)
			}
		})
	}
}
