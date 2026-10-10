package app

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestBuiltinImages(t *testing.T) {
	for _, tt := range []struct {
		name, input, release, wantErr string
	}{
		{name: "unconfigured", input: "{}", release: "dev"},
		{name: "local defaults", input: `{"kagent":"registry/image@sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}`, release: "dev"},
		{name: "matching release", input: `{"release":"1.0.0-alpha9"}`, release: "v1.0.0-alpha9"},
		{name: "wrong release", input: `{"release":"1.0.0-alpha8"}`, release: "1.0.0-alpha9", wantErr: "does not match controller release"},
		{name: "malformed", input: "{", release: "dev", wantErr: "failed to decode"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			_, err := builtinImages(tt.input, tt.release)
			if tt.wantErr != "" {
				require.ErrorContains(t, err, tt.wantErr)
			} else {
				require.NoError(t, err)
			}
		})
	}
}
