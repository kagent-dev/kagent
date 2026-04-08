package a2a

import (
	"strings"

	"trpc.group/trpc-go/trpc-a2a-go/protocol"
)

// ExtractText extracts the text content from a message.
func ExtractText(message protocol.Message) string {
	builder := strings.Builder{}
	for _, part := range message.Parts {
		if textPart, ok := part.(*protocol.TextPart); ok {
			builder.WriteString(textPart.Text)
		}
	}
	return builder.String()
}
