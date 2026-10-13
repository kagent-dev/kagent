package translator

import (
	configv1 "github.com/kagent-dev/kagent/go/api/gen/kagent/config/v1"
	"github.com/kagent-dev/kagent/go/core/internal/image"
)

func harnessImage(harness *HarnessConfiguration, images *configv1.HarnessImagesConfig) (string, error) {
	var selected string
	if override := harness.Spec.Workload.Image; override != nil {
		selected = *override
	} else {
		switch harnessType(harness) {
		case HarnessTypeKagent:
			selected = images.GetKagent()
		case HarnessTypeClaude:
			selected = images.GetClaude()
		case HarnessTypeCodex:
			selected = images.GetCodex()
		case HarnessTypeBYO:
			return "", NewValidationError("BYO harnesses must specify workload.image")
		}
		if selected == "" {
			return "", NewValidationError("no default image configured for %s Harness; configure the release's built-in images or set workload.image", harnessType(harness))
		}
	}
	if !image.IsDigestPinned(selected) {
		return "", NewValidationError("Harness image must be pinned by sha256 digest")
	}
	return selected, nil
}
