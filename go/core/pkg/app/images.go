package app

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/kagent-dev/kagent/go/core/internal/translator"
)

func builtinImages(raw, release string) (translator.BuiltinImages, error) {
	var images translator.BuiltinImages
	if err := json.Unmarshal([]byte(raw), &images); err != nil {
		return images, fmt.Errorf("failed to decode built-in Harness images: %w", err)
	}
	if images.Release != "" && strings.TrimPrefix(images.Release, "v") != strings.TrimPrefix(release, "v") {
		return images, fmt.Errorf("built-in Harness images release %q does not match controller release %q", images.Release, release)
	}
	return images, nil
}
