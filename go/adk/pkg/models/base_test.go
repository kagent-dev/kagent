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

func TestExtractFunctionResponseContent(t *testing.T) {
	tests := []struct {
		name string
		resp any
		want string
	}{
		{
			name: "plain string response",
			resp: "hello",
			want: "hello",
		},
		{
			name: "content array with top-level text items",
			resp: map[string]any{
				"content": []any{
					map[string]any{"type": "text", "text": "line1"},
					map[string]any{"type": "text", "text": "line2"},
				},
			},
			want: "line1\nline2",
		},
		{
			name: "resource content nested under resource.text (GitHub MCP get_file_contents)",
			resp: map[string]any{
				"content": []any{
					map[string]any{"type": "text", "text": "successfully downloaded text file (SHA: abc123)"},
					map[string]any{
						"type": "resource",
						"resource": map[string]any{
							"uri":  "repo://owner/repo/contents/path.yaml",
							"text": "p, role:foo-developer, applications, get, *, allow",
						},
					},
				},
			},
			want: "successfully downloaded text file (SHA: abc123)\np, role:foo-developer, applications, get, *, allow",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := extractFunctionResponseContent(tt.resp)
			if got != tt.want {
				t.Errorf("extractFunctionResponseContent() = %q, want %q", got, tt.want)
			}
		})
	}
}
