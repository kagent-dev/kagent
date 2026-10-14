package models

import (
	"context"
	"testing"

	"github.com/a2aproject/a2a-go/v2/a2asrv"
	"github.com/stretchr/testify/require"
	"google.golang.org/genai"

	apia2a "github.com/kagent-dev/kagent/go/api/a2a"
)

// A caller that authenticates with its bearer token sends the model key in its
// own header; passthrough must pick the model key, never the credential.
func TestPassthroughTokenPrefersModelAPIKey(t *testing.T) {
	for _, tt := range []struct {
		name        string
		bearer      string
		modelAPIKey string
		passthrough bool
		want        string
	}{
		{name: "model key wins over bearer", bearer: "user-credential", modelAPIKey: "model-key", passthrough: true, want: "model-key"},
		{name: "bearer when no model key", bearer: "federated-token", passthrough: true, want: "federated-token"},
		{name: "nothing without passthrough", bearer: "user-credential", modelAPIKey: "model-key"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			ctx, _ := a2asrv.NewCallContext(context.Background(), a2asrv.NewServiceParams(map[string][]string{
				apia2a.ModelAPIKeyHeader: {tt.modelAPIKey},
			}))
			if key := ModelAPIKeyFromCallContext(ctx); key != "" {
				ctx = context.WithValue(ctx, ModelAPIKeyKey, key)
			}
			if tt.bearer != "" {
				ctx = context.WithValue(ctx, BearerTokenKey, tt.bearer)
			}
			got, ok := PassthroughToken(ctx, tt.passthrough)
			require.Equal(t, tt.want, got)
			require.Equal(t, tt.want != "", ok)
			// STS and MCP read the caller's credential, never the model key.
			require.Equal(t, tt.bearer, BearerTokenFromContext(ctx))
		})
	}
}

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
