package app

import (
	"fmt"
	"strings"

	apiv1 "github.com/kagent-dev/kagent/go/api/gen/kagent/api/v1alpha1"
)

func validateHarnessImagesRelease(cfg *apiv1.HarnessImagesConfig, release string) error {
	if cfg.GetRelease() != "" && strings.TrimPrefix(cfg.GetRelease(), "v") != strings.TrimPrefix(release, "v") {
		return fmt.Errorf("built-in Harness images release %q does not match controller release %q", cfg.GetRelease(), release)
	}
	return nil
}
