package app

import (
	"testing"

	"github.com/stretchr/testify/require"

	apiv1 "github.com/kagent-dev/kagent/go/api/gen/kagent/api/v1alpha1"
)

func TestValidateHarnessImagesRelease(t *testing.T) {
	for _, tt := range []struct {
		name, release, wantErr string
		input                  *apiv1.HarnessImagesConfig
	}{
		{name: "unconfigured", release: "dev"},
		{name: "local defaults", input: &apiv1.HarnessImagesConfig{Kagent: "registry/image@sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}, release: "dev"},
		{name: "matching release", input: &apiv1.HarnessImagesConfig{Release: "1.0.0-alpha9"}, release: "v1.0.0-alpha9"},
		{name: "wrong release", input: &apiv1.HarnessImagesConfig{Release: "1.0.0-alpha8"}, release: "1.0.0-alpha9", wantErr: "does not match controller release"},
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
