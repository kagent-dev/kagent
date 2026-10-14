package app

import (
	"fmt"
	"strings"

	apiv1 "github.com/kagent-dev/kagent/go/api/gen/kagent/api/v1alpha1"
	"github.com/kagent-dev/kagent/go/core/internal/translator"
)

func builtinImages(cfg *apiv1.HarnessImagesConfig, release string) (translator.BuiltinImages, error) {
	if cfg.GetRelease() != "" && strings.TrimPrefix(cfg.GetRelease(), "v") != strings.TrimPrefix(release, "v") {
		return translator.BuiltinImages{}, fmt.Errorf("built-in Harness images release %q does not match controller release %q", cfg.GetRelease(), release)
	}
	return translator.BuiltinImages{Kagent: cfg.GetKagent(), Claude: cfg.GetClaude(), Codex: cfg.GetCodex()}, nil
}
