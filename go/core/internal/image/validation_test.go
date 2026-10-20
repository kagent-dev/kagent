package image_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/kagent-dev/kagent/go/core/internal/image"
)

func TestIsDigestPinned(t *testing.T) {
	const digest = "@sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	for _, tt := range []struct {
		name      string
		reference string
		want      bool
	}{
		{"pinned", "registry/image" + digest, true},
		{"registry port", "localhost:5001/team/image" + digest, true},
		{"tag and digest", "registry/image:tag" + digest, true},
		{"empty", "", false},
		{"tag only", "registry/image:latest", false},
		{"missing repository", digest, false},
		{"short digest", "registry/image" + digest[:len(digest)-1], false},
		{"uppercase digest", "registry/image@sha256:" + strings.Repeat("A", 64), false},
		{"multiple digests", "registry/image" + digest + digest, false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.want, image.IsDigestPinned(tt.reference))
		})
	}
	for _, whitespace := range " \t\n\r\v\f" {
		require.False(t, image.IsDigestPinned("registry/"+string(whitespace)+"image"+digest))
	}
}
