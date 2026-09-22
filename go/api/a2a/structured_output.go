package a2a

import (
	"encoding/json"
	"fmt"
	"strings"

	a2atype "github.com/a2aproject/a2a-go/v2/a2a"
)

// OutputSchemaSHA256MetadataKey identifies the canonical schema enforced for a
// structured terminal result.
const OutputSchemaSHA256MetadataKey = "kagent.dev/output-schema-sha256"

// IsStructuredOutputPart reports whether a part carries kagent's structured
// terminal-result signature.
func IsStructuredOutputPart(part *a2atype.Part) bool {
	if part == nil {
		return false
	}
	if _, ok := part.Content.(a2atype.Data); !ok {
		return false
	}
	mediaType, _, _ := strings.Cut(part.MediaType, ";")
	if !strings.EqualFold(strings.TrimSpace(mediaType), "application/json") {
		return false
	}
	_, ok := part.Metadata[OutputSchemaSHA256MetadataKey].(string)
	return ok
}

// StructuredOutputJSON serializes a structured terminal result for consumers
// whose output contract is text, such as the CLI and remote-agent tools.
func StructuredOutputJSON(part *a2atype.Part) (string, error) {
	if !IsStructuredOutputPart(part) {
		return "", fmt.Errorf("part is not structured output")
	}
	encoded, err := json.Marshal(part.Data())
	if err != nil {
		return "", fmt.Errorf("marshal structured output: %w", err)
	}
	return string(encoded), nil
}
