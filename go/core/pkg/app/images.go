package app

import (
	"fmt"
	"strings"

	configv1 "github.com/kagent-dev/kagent/go/api/gen/kagent/config/v1"
)

func validateHarnessImagesRelease(cfg *configv1.HarnessImagesConfig, release string) error {
	if cfg.GetRelease() != "" && strings.TrimPrefix(cfg.GetRelease(), "v") != strings.TrimPrefix(release, "v") {
		return fmt.Errorf("built-in Harness images release %q does not match controller release %q", cfg.GetRelease(), release)
	}
	return nil
}
