package tracing

import (
	"encoding/json"
	"strings"
	"unicode/utf8"
)

// Message roles in the GenAI conventions' message shape.
const (
	RoleUser      = "user"
	RoleAssistant = "assistant"
)

// Finish reasons the GenAI conventions define for an output message. An
// outcome the enumeration does not describe is recorded under its own name,
// which the conventions permit.
const (
	FinishReasonStop     = "stop"
	FinishReasonToolCall = "tool_call"
	FinishReasonError    = "error"
)

type messagePart struct {
	Type    string `json:"type"`
	Content string `json:"content"`
}

type inputMessage struct {
	Role  string        `json:"role"`
	Parts []messagePart `json:"parts"`
}

// outputMessage carries the finish reason the conventions require on every
// output message, so one cannot be rendered without it.
type outputMessage struct {
	Role         string        `json:"role"`
	Parts        []messagePart `json:"parts"`
	FinishReason string        `json:"finish_reason"`
}

// InputMessages renders one user text message as the JSON document the GenAI
// conventions define for gen_ai.input.messages: an array of messages, each
// with a role and parts. The capture budget bounds the text; the few dozen
// bytes of structure around it are not counted, so a consumer that parses the
// document gets the whole bounded text.
func InputMessages(text string) string {
	// A message of strings always marshals.
	encoded, _ := json.Marshal([]inputMessage{{Role: RoleUser, Parts: textParts(text)}})
	return string(encoded)
}

// OutputMessages renders one assistant text message as the JSON document the
// conventions define for gen_ai.output.messages, with the reason the segment
// finished. The same budget rule as InputMessages applies.
func OutputMessages(text, finishReason string) string {
	encoded, _ := json.Marshal([]outputMessage{{Role: RoleAssistant, Parts: textParts(text), FinishReason: finishReason}})
	return string(encoded)
}

func textParts(text string) []messagePart {
	return []messagePart{{Type: "text", Content: text}}
}

// TextCapture accumulates the beginning of a text stream up to a byte limit.
// Once the limit is reached it records that fact and discards further input
// without retaining or copying it, so the memory one capture can hold stays
// proportional to the limit regardless of response length or delta count.
// The zero value is unusable; construct it with NewTextCapture.
type TextCapture struct {
	limit     int
	buffer    []byte
	truncated bool
}

// NewTextCapture returns a collector bounded to limit bytes, or nil when limit
// is not positive. A nil collector accepts appends and yields no text, so
// callers do not branch on whether capture is enabled.
func NewTextCapture(limit int) *TextCapture {
	if limit <= 0 {
		return nil
	}
	return &TextCapture{limit: limit}
}

// Append adds as much of text as the remaining budget holds. Nothing is
// accepted after the first truncation, because a budget that could still hold
// a later, shorter fragment would otherwise splice text that was never
// adjacent in the stream.
func (c *TextCapture) Append(text string) {
	if c == nil || text == "" || c.truncated {
		return
	}
	remaining := c.limit - len(c.buffer)
	if remaining <= 0 {
		c.truncated = true
		return
	}
	if len(text) <= remaining {
		if c.buffer == nil {
			c.buffer = make([]byte, 0, min(c.limit, len(text)*2))
		}
		c.buffer = append(c.buffer, text...)
		return
	}
	if c.buffer == nil {
		c.buffer = make([]byte, 0, c.limit)
	}
	c.buffer = append(c.buffer, text[:runeBoundary(text, remaining)]...)
	c.truncated = true
}

// Text returns the captured prefix.
func (c *TextCapture) Text() string {
	if c == nil {
		return ""
	}
	return string(c.buffer)
}

// Truncated reports whether any input was dropped.
func (c *TextCapture) Truncated() bool {
	return c != nil && c.truncated
}

// BoundedText returns the longest prefix of text within limit bytes and
// whether it was shortened. A limit that is not positive yields no text.
//
// The prefix is copied. A slice of the original would keep the whole input
// alive for as long as the span holding it is buffered, which would make the
// limit bound the exported value rather than the memory capture costs.
func BoundedText(text string, limit int) (string, bool) {
	if limit <= 0 {
		return "", text != ""
	}
	if len(text) <= limit {
		return text, false
	}
	return strings.Clone(text[:runeBoundary(text, limit)]), true
}

// runeBoundary returns the largest cut point at or below limit that does not
// split a UTF-8 encoded rune.
func runeBoundary(text string, limit int) int {
	if limit >= len(text) {
		return len(text)
	}
	for cut := limit; cut > 0; cut-- {
		if utf8.RuneStart(text[cut]) {
			return cut
		}
	}
	return 0
}
