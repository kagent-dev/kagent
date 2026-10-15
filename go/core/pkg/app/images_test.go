package app

import (
	"testing"

	"github.com/stretchr/testify/require"

	configv1 "github.com/kagent-dev/kagent/go/api/gen/kagent/config/v1"
)

func TestValidateHarnessImagesRelease(t *testing.T) {
	for _, tt := range []struct {
		name, release, wantErr string
		input                  *configv1.HarnessImagesConfig
	}{
		{name: "unconfigured", release: "dev"},
		{name: "local defaults", input: &configv1.HarnessImagesConfig{Kagent: "registry/image@sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}, release: "dev"},
		{name: "matching release", input: &configv1.HarnessImagesConfig{Release: "1.0.0-alpha9"}, release: "v1.0.0-alpha9"},
		{name: "wrong release", input: &configv1.HarnessImagesConfig{Release: "1.0.0-alpha8"}, release: "1.0.0-alpha9", wantErr: "does not match controller release"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			err := validateHarnessImagesRelease(tt.input, tt.release)
			if tt.wantErr != "" {
				require.ErrorContains(t, err, tt.wantErr)
			} else {
				require.NoError(t, err)
			}
		})
	}
}
