package translator

import "github.com/kagent-dev/kagent/go/core/internal/image"

// BuiltinImages supplies release images without performing registry I/O.
// Missing entries only prevent compilation of agents that need that default.
type BuiltinImages struct {
	Kagent string
	Claude string
	Codex  string
}

func (b BuiltinImages) image(harness *HarnessConfiguration) (string, error) {
	var selected string
	if override := harness.Spec.Workload.Image; override != nil {
		selected = *override
	} else {
		switch harnessType(harness) {
		case HarnessTypeKagent:
			selected = b.Kagent
		case HarnessTypeClaude:
			selected = b.Claude
		case HarnessTypeCodex:
			selected = b.Codex
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
