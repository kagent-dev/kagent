package agent

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestResolveOpenAIAPIKey(t *testing.T) {
	tests := []struct {
		name        string
		env         string
		passthrough bool
		baseURL     string
		want        string
		wantErr     bool
	}{
		{
			name:    "no key and no base URL fails",
			wantErr: true,
		},
		{
			name:    "no key with custom base URL is unauthenticated",
			baseURL: "http://vllm.default.svc/v1",
			want:    "",
		},
		{
			name:    "env key wins over an unauthenticated client",
			env:     "sk-test",
			baseURL: "http://vllm.default.svc/v1",
			want:    "sk-test",
		},
		{
			name: "env key without base URL",
			env:  "sk-test",
			want: "sk-test",
		},
		{
			name:        "passthrough ignores env and base URL",
			passthrough: true,
			want:        "passthrough",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("OPENAI_API_KEY", tt.env)
			got, err := resolveOpenAIAPIKey(t.Context(), tt.passthrough, tt.baseURL)
			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tt.want, got)
		})
	}
}
