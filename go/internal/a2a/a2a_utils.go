package a2a

import (
	"context"
	"iter"
	"strings"

	"github.com/a2aproject/a2a-go/a2a"
)

// ExtractText extracts the text content from a message.
func ExtractText(message *a2a.Message) string {
	builder := strings.Builder{}
	for _, part := range message.Parts {
		if textPart, ok := part.(*a2a.TextPart); ok {
			builder.WriteString(textPart.Text)
		}
	}
	return builder.String()
}

// EventIterToChannel converts an iter.Seq2[a2a.Event, error] to a channel.
// This is needed because the TUI/CLI code uses channels for streaming events,
// while a2a-go uses iter.Seq2 iterators.
func EventIterToChannel(ctx context.Context, eventIter iter.Seq2[a2a.Event, error]) <-chan a2a.Event {
	channel := make(chan a2a.Event, 64)
	go func() {
		defer close(channel)
		for event, err := range eventIter {
			if err != nil {
				break
			}
			select {
			case channel <- event:
			case <-ctx.Done():
				return
			}
		}
	}()
	return channel
}
