package translator

import "regexp"

// BuiltinImages supplies release images without performing registry I/O.
// Missing entries only prevent compilation of agents that need that default.
type BuiltinImages struct {
	Release string `json:"release"`
	Kagent  string `json:"kagent"`
	Claude  string `json:"claude"`
	Codex   string `json:"codex"`
}

var pinnedHarnessImage = regexp.MustCompile(`^[^[:space:]@]+@sha256:[a-f0-9]{64}$`)

func (b BuiltinImages) image(harness *HarnessConfiguration) (string, error) {
	var image string
	if override := harness.Spec.Workload.Image; override != nil {
		image = *override
	} else {
		switch harnessType(harness) {
		case HarnessTypeKagent:
			image = b.Kagent
		case HarnessTypeClaude:
			image = b.Claude
		case HarnessTypeCodex:
			image = b.Codex
		case HarnessTypeBYO:
			return "", NewValidationError("BYO harnesses must specify workload.image")
		}
		if image == "" {
			return "", NewValidationError("no default image configured for %s Harness; configure the release's built-in images or set workload.image", harnessType(harness))
		}
	}
	if !pinnedHarnessImage.MatchString(image) {
		return "", NewValidationError("Harness image must be pinned by sha256 digest")
	}
	return image, nil
}
