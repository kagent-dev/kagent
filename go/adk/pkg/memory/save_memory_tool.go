package memory

import (
	adkagent "google.golang.org/adk/v2/agent"
	"google.golang.org/adk/v2/tool"
	"google.golang.org/adk/v2/tool/functiontool"
)

type saveMemoryInput struct {
	Content string `json:"content"`
}

// NewSaveMemoryTool creates a save_memory tool backed by the given memory service.
func NewSaveMemoryTool(svc *KagentMemoryService) (tool.Tool, error) {
	return functiontool.New(functiontool.Config{
		Name:        "save_memory",
		Description: "Saves a specific piece of information or text to long-term memory. Use this to remember important facts, user preferences, or specific details for future reference.",
	}, func(toolCtx adkagent.Context, in saveMemoryInput) (map[string]any, error) {
		// SaveMemoryItem emits the memory.write span for this explicit save. The tool
		// is a thin adapter so the span-bearing write path stays unit-testable without
		// constructing a full ADK ToolContext.
		if err := svc.SaveMemoryItem(toolCtx, toolCtx.UserID(), in.Content); err != nil {
			return nil, err
		}
		return map[string]any{"status": "Successfully saved information to long-term memory."}, nil
	})
}
