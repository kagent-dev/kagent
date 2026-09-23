package a2a

import (
	"encoding/json"
	"fmt"
	"strings"

	a2atype "github.com/a2aproject/a2a-go/v2/a2a"
)

const outputSchemaSHA256MetadataKey = "kagent.dev/a2a/output-schema-sha256"

// NewStructuredOutputPart constructs a structured terminal result with its
// complete wire signature: JSON data and the digest of the enforced schema.
func NewStructuredOutputPart(value any, schemaSHA256 string) *a2atype.Part {
	part := a2atype.NewDataPart(value)
	part.MediaType = "application/json"
	part.SetMeta(outputSchemaSHA256MetadataKey, schemaSHA256)
	return part
}

// StructuredOutputSchemaSHA256 returns the digest carried by a structured
// terminal result. Ordinary data parts and incomplete signatures are rejected.
func StructuredOutputSchemaSHA256(part *a2atype.Part) (string, bool) {
	if part == nil {
		return "", false
	}
	if _, ok := part.Content.(a2atype.Data); !ok {
		return "", false
	}
	mediaType, _, _ := strings.Cut(part.MediaType, ";")
	if !strings.EqualFold(strings.TrimSpace(mediaType), "application/json") {
		return "", false
	}
	digest, ok := part.Metadata[outputSchemaSHA256MetadataKey].(string)
	return digest, ok
}

// IsStructuredOutputPart reports whether a part carries kagent's structured
// terminal-result signature.
func IsStructuredOutputPart(part *a2atype.Part) bool {
	_, ok := StructuredOutputSchemaSHA256(part)
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
