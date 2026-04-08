package memory

import (
	"fmt"

	"google.golang.org/adk/tool"
	"google.golang.org/adk/tool/functiontool"
)

type saveMemoryInput struct {
	Content string `json:"content"`
}

// NewSaveMemoryTool creates a save_memory tool backed by the given memory service.
func NewSaveMemoryTool(svc *KagentMemoryService) (tool.Tool, error) {
	return functiontool.New(functiontool.Config{
		Name:        "save_memory",
		Description: "Saves a specific piece of information or text to long-term memory. Use this to remember important facts, user preferences, or specific details for future reference.",
	}, func(toolCtx tool.Context, in saveMemoryInput) (map[string]any, error) {
		if in.Content == "" {
			return nil, fmt.Errorf("missing required parameter: content")
		}

		// Generate embedding for the content.
		embeddings, err := svc.embeddingClient.Generate(toolCtx, []string{in.Content})
		if err != nil {
			return nil, fmt.Errorf("failed to generate embedding: %w", err)
		}
		var vector []float32
		if len(embeddings) > 0 {
			vector = embeddings[0]
		}
		if vector == nil {
			return nil, fmt.Errorf("embedding generation returned no vectors")
		}

		if err := svc.storeMemory(toolCtx, toolCtx.UserID(), in.Content, vector); err != nil {
			return nil, fmt.Errorf("failed to save memory: %w", err)
		}

		return map[string]any{"status": "Successfully saved information to long-term memory."}, nil
	})
}
