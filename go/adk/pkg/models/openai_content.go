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
			openAITextChunk
			Thinking []openAITextChunk `json:"thinking"`
		}
		if err := json.Unmarshal(rawChunk, &chunk); err != nil {
			return nil, fmt.Errorf("failed to decode OpenAI content chunk: %w", err)
		}
		switch chunk.Type {
		case openAIChunkTypeText:
			if chunk.Text != "" {
				parts = append(parts, &genai.Part{Text: chunk.Text})
			}
		case openAIChunkTypeThinking:
			var thought strings.Builder
			for _, inner := range chunk.Thinking {
				if inner.Type != openAIChunkTypeText {
					return nil, fmt.Errorf("unsupported OpenAI thinking chunk type %q", inner.Type)
				}
				thought.WriteString(inner.Text)
			}
			parts = append(parts, &genai.Part{
				Text: thought.String(), Thought: true,
				// Preserve the native block, including closed, across persistence.
				PartMetadata: map[string]any{mistralThinkingChunkKey: string(rawChunk)},
			})
		default:
			return nil, fmt.Errorf("unsupported OpenAI content chunk type %q", chunk.Type)
		}
	}
	return parts, nil
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
