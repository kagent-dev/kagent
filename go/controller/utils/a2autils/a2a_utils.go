package a2autils

import "trpc.group/trpc-go/trpc-a2a-go/protocol"

// ExtractText extracts the text content from a message.
func ExtractText(message protocol.Message) string {
	for _, part := range message.Parts {
		if textPart, ok := part.(protocol.TextPart); ok {
			return textPart.Text
		}
	}
	return ""
}
