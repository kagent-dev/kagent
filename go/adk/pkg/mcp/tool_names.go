package mcp

import (
	"crypto/sha256"
	"fmt"
	"regexp"

	adkagent "google.golang.org/adk/v2/agent"
	"google.golang.org/adk/v2/model"
	"google.golang.org/adk/v2/tool"
	"google.golang.org/adk/v2/tool/toolutils"
	"google.golang.org/genai"
)

var invalidFunctionNameChar = regexp.MustCompile(`[^a-zA-Z0-9_-]`)

func (m *mcpAppToolset) toolName(name string) string {
	// Keep the alias within the model's function name limit, including when
	// two original names have the same sanitized or truncated spelling.
	const maxLength = 64
	digest := sha256.Sum256([]byte(name))
	suffix := fmt.Sprintf("_%x", digest[:6])
	readable := invalidFunctionNameChar.ReplaceAllString(name, "_")
	return m.toolPrefix + readable[:min(len(readable), maxLength-len(m.toolPrefix)-len(suffix))] + suffix
}

func (m *mcpAppToolset) aliasTools(tools []tool.Tool) ([]tool.Tool, error) {
	if m.toolPrefix == "" || len(tools) == 0 {
		return tools, nil
	}
	aliased := make([]tool.Tool, len(tools))
	for i, t := range tools {
		fn, ok := t.(functionTool)
		if !ok {
			return nil, fmt.Errorf("MCP tool %q does not implement the ADK function tool interface", t.Name())
		}
		aliased[i] = aliasedTool{functionTool: fn, name: m.toolName(t.Name())}
	}
	return aliased, nil
}

// aliasedTool changes registration in ADK while delegating approval and
// execution to the existing tool, which uses the original MCP wire name.
type aliasedTool struct {
	functionTool
	name string
}

var _ functionTool = aliasedTool{}

func (a aliasedTool) Name() string { return a.name }

func (a aliasedTool) Declaration() *genai.FunctionDeclaration {
	original := a.functionTool.Declaration()
	if original == nil {
		return nil
	}
	decl := *original
	decl.Name = a.name
	return &decl
}

func (a aliasedTool) ProcessRequest(_ adkagent.Context, req *model.LLMRequest) error {
	return toolutils.PackTool(req, a)
}
