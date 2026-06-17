package a2a

import (
	"strings"

	a2atype "github.com/a2aproject/a2a-go/v2/a2a"
)

// ExtractText extracts the text content from a message.
func ExtractText(message *a2atype.Message) string {
	if message == nil {
		return ""
	}
	builder := strings.Builder{}
	for _, part := range message.Parts {
		if part != nil {
			builder.WriteString(part.Text())
		}
	}
	return builder.String()
}
