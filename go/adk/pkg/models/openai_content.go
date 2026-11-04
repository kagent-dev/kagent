package models

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/packages/param"
	"google.golang.org/genai"
)

const (
	openAIContentError      = "CONTENT_ERROR"
	openAIChunkTypeText     = "text"
	openAIChunkTypeThinking = "thinking"
	mistralThinkingChunkKey = "mistral_thinking_chunk"
)

type openAITextChunk struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

type mistralThinkingChunk struct {
	Type     string            `json:"type"`
	Thinking []json.RawMessage `json:"thinking"`
	Closed   bool              `json:"closed"`
}

// The SDK models content as a string, but Mistral returns typed chunk arrays.
// Inspect the wire JSON so a string containing JSON remains ordinary answer text.
func openAIContentToParts(text, raw string) ([]*genai.Part, error) {
	if !strings.HasPrefix(strings.TrimSpace(raw), "[") {
		if text == "" {
			return nil, nil
		}
		return []*genai.Part{{Text: text}}, nil
	}
	var chunks []json.RawMessage
	if err := json.Unmarshal([]byte(raw), &chunks); err != nil {
		return nil, fmt.Errorf("failed to decode OpenAI content chunks: %w", err)
	}
	parts := make([]*genai.Part, 0, len(chunks))
	for _, rawChunk := range chunks {
		var chunk struct {
			Type string `json:"type"`
		}
		if err := json.Unmarshal(rawChunk, &chunk); err != nil {
			return nil, fmt.Errorf("failed to decode OpenAI content chunk: %w", err)
		}
		switch chunk.Type {
		case openAIChunkTypeText:
			var textChunk openAITextChunk
			if err := json.Unmarshal(rawChunk, &textChunk); err != nil {
				return nil, fmt.Errorf("failed to decode OpenAI text chunk: %w", err)
			}
			if textChunk.Text != "" {
				parts = append(parts, &genai.Part{Text: textChunk.Text})
			}
		case openAIChunkTypeThinking:
			var thinkingChunk mistralThinkingChunk
			if err := json.Unmarshal(rawChunk, &thinkingChunk); err != nil {
				return nil, fmt.Errorf("failed to decode OpenAI thinking chunk: %w", err)
			}
			var thought strings.Builder
			for _, rawInner := range thinkingChunk.Thinking {
				var inner struct {
					Type string `json:"type"`
				}
				if err := json.Unmarshal(rawInner, &inner); err != nil {
					return nil, fmt.Errorf("failed to decode OpenAI thinking content: %w", err)
				}
				if inner.Type == "" {
					return nil, fmt.Errorf("OpenAI thinking content is missing its type")
				}
				if inner.Type == openAIChunkTypeText {
					var textChunk openAITextChunk
					if err := json.Unmarshal(rawInner, &textChunk); err != nil {
						return nil, fmt.Errorf("failed to decode OpenAI thinking text: %w", err)
					}
					thought.WriteString(textChunk.Text)
				}
			}
			parts = append(parts, &genai.Part{
				Text: thought.String(), Thought: true,
				// Preserve the native block, including closed, across persistence.
				PartMetadata: map[string]any{mistralThinkingChunkKey: string(rawChunk)},
			})
		case "":
			return nil, fmt.Errorf("OpenAI content chunk is missing its type")
		default:
			// Unsupported non-text extensions must not abort the answer or become text.
			continue
		}
	}
	return parts, nil
}

// Complete a streamed thinking block before persisting or replaying the turn.
func mergeMistralThinkingParts(parts []*genai.Part) ([]*genai.Part, error) {
	var merged *genai.Part
	var text strings.Builder
	var thinking []json.RawMessage
	var result []*genai.Part
	for _, part := range parts {
		if !part.Thought {
			result = append(result, part)
			continue
		}
		var chunk mistralThinkingChunk
		raw, _ := part.PartMetadata[mistralThinkingChunkKey].(string)
		if err := json.Unmarshal([]byte(raw), &chunk); err != nil {
			return nil, fmt.Errorf("failed to decode streamed Mistral thinking chunk: %w", err)
		}
		thinking = append(thinking, chunk.Thinking...)
		text.WriteString(part.Text)
		if merged == nil {
			merged = &genai.Part{Thought: true}
			result = append(result, merged)
		}
	}
	if merged != nil {
		chunk := mistralThinkingChunk{Type: openAIChunkTypeThinking, Thinking: thinking, Closed: true}
		raw, err := json.Marshal(chunk)
		if err != nil {
			return nil, fmt.Errorf("failed to encode complete Mistral thinking chunk: %w", err)
		}
		merged.Text = text.String()
		merged.PartMetadata = map[string]any{mistralThinkingChunkKey: string(raw)}
	}
	return result, nil
}

// Only native Mistral thoughts opt a message into Mistral's chunk-list format.
func mistralAssistantContent(parts []*genai.Part) []openai.ChatCompletionAssistantMessageParamContentArrayOfContentPartUnion {
	var chunks []openai.ChatCompletionAssistantMessageParamContentArrayOfContentPartUnion
	var hasThinking bool
	for _, part := range parts {
		if part == nil {
			continue
		}
		if part.Thought {
			if raw, ok := part.PartMetadata[mistralThinkingChunkKey].(string); ok {
				hasThinking = true
				chunks = append(chunks, param.Override[openai.ChatCompletionAssistantMessageParamContentArrayOfContentPartUnion](json.RawMessage(raw)))
			}
		} else if part.Text != "" {
			chunks = append(chunks, openai.ChatCompletionAssistantMessageParamContentArrayOfContentPartUnion{
				OfText: &openai.ChatCompletionContentPartTextParam{Text: part.Text},
			})
		}
	}
	if !hasThinking {
		return nil
	}
	return chunks
}
