package runtime

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestOmitImageData(t *testing.T) {
	for _, test := range []struct {
		name        string
		value, want any
	}{
		{"claude image",
			[]any{map[string]any{"type": "text", "text": "image/png, 3×2 px"}, map[string]any{"type": "image", "source": map[string]any{"type": "base64", "media_type": "image/png", "data": "AAAABBBB"}}},
			[]any{map[string]any{"type": "text", "text": "image/png, 3×2 px"}, map[string]any{"type": "image", "source": map[string]any{"type": "base64", "media_type": "image/png", "omitted": "image data", "bytes": 6}}}},
		{"mcp image in a codex result",
			map[string]any{"status": "completed", "result": map[string]any{"content": []any{map[string]any{"type": "image", "mimeType": "image/jpeg", "data": "AAAA"}}}},
			map[string]any{"status": "completed", "result": map[string]any{"content": []any{map[string]any{"type": "image", "mimeType": "image/jpeg", "omitted": "image data", "bytes": 3}}}}},
		{"data outside an image is kept",
			map[string]any{"type": "resource", "data": "AAAA"},
			map[string]any{"type": "resource", "data": "AAAA"}},
		{"scalar", "text", "text"},
		{"nil", nil, nil},
	} {
		t.Run(test.name, func(t *testing.T) {
			require.Equal(t, test.want, OmitImageData(test.value))
		})
	}
}
