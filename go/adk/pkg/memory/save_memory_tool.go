package memory

import (
	"fmt"

	"google.golang.org/adk/model"
	"google.golang.org/adk/tool"
	"google.golang.org/genai"
)

// saveMemoryTool implements a tool that saves content to long-term memory
// via the KagentMemoryService. This is needed because the upstream Google ADK
// tool.Context only exposes SearchMemory, not a save/add method.
type saveMemoryTool struct {
	svc *KagentMemoryService
}

// NewSaveMemoryTool creates a save_memory tool backed by the given memory service.
func NewSaveMemoryTool(svc *KagentMemoryService) tool.Tool {
	return &saveMemoryTool{svc: svc}
}

func (t *saveMemoryTool) Name() string {
	return "save_memory"
}

func (t *saveMemoryTool) Description() string {
	return "Saves a specific piece of information or text to long-term memory. Use this to remember important facts, user preferences, or specific details for future reference."
}

func (t *saveMemoryTool) IsLongRunning() bool {
	return false
}

// Declaration returns the function declaration for the LLM.
func (t *saveMemoryTool) Declaration() *genai.FunctionDeclaration {
	return &genai.FunctionDeclaration{
		Name:        t.Name(),
		Description: t.Description(),
		Parameters: &genai.Schema{
			Type: "OBJECT",
			Properties: map[string]*genai.Schema{
				"content": {
					Type:        "STRING",
					Description: "The text content or fact to save to memory.",
				},
			},
			Required: []string{"content"},
		},
	}
}

// Run saves the content to memory with an embedding vector.
func (t *saveMemoryTool) Run(toolCtx tool.Context, args any) (map[string]any, error) {
	m, ok := args.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("unexpected args type, got: %T", args)
	}

	contentRaw, exists := m["content"]
	if !exists {
		return nil, fmt.Errorf("missing required parameter: content")
	}

	content, ok := contentRaw.(string)
	if !ok {
		return nil, fmt.Errorf("content must be a string, got: %T", contentRaw)
	}

	// Generate embedding for the content.
	embeddings, err := t.svc.embeddingClient.Generate(toolCtx, []string{content})
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

	if err := t.svc.storeMemory(toolCtx, toolCtx.UserID(), content, vector); err != nil {
		return nil, fmt.Errorf("failed to save memory: %w", err)
	}

	return map[string]any{"status": "Successfully saved information to long-term memory."}, nil
}

// ProcessRequest packs the tool's function declaration into the LLM request.
// The Google ADK runtime requires this interface for custom tools.
func (t *saveMemoryTool) ProcessRequest(_ tool.Context, req *model.LLMRequest) error {
	if req.Tools == nil {
		req.Tools = make(map[string]any)
	}
	if _, ok := req.Tools[t.Name()]; ok {
		return fmt.Errorf("duplicate tool: %q", t.Name())
	}
	req.Tools[t.Name()] = t

	if req.Config == nil {
		req.Config = &genai.GenerateContentConfig{}
	}
	// Find an existing genai.Tool with FunctionDeclarations or create one.
	var funcTool *genai.Tool
	for _, gt := range req.Config.Tools {
		if gt != nil && gt.FunctionDeclarations != nil {
			funcTool = gt
			break
		}
	}
	if funcTool == nil {
		req.Config.Tools = append(req.Config.Tools, &genai.Tool{
			FunctionDeclarations: []*genai.FunctionDeclaration{t.Declaration()},
		})
	} else {
		funcTool.FunctionDeclarations = append(funcTool.FunctionDeclarations, t.Declaration())
	}
	return nil
}
