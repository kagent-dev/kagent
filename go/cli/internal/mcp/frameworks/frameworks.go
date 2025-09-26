package frameworks

import (
	"fmt"

	"github.com/kagent-dev/kagent/go/cli/internal/mcp"
	"github.com/kagent-dev/kagent/go/cli/internal/mcp/frameworks/golang"
	"github.com/kagent-dev/kagent/go/cli/internal/mcp/frameworks/java"
	"github.com/kagent-dev/kagent/go/cli/internal/mcp/frameworks/python"
	"github.com/kagent-dev/kagent/go/cli/internal/mcp/frameworks/typescript"
)

// Generator defines the interface for a framework-specific generator.
type Generator interface {
	GenerateProject(config mcp.ProjectConfig) error
	GenerateTool(projectRoot string, config mcp.ToolConfig) error
}

// GetGenerator returns a generator for the specified framework.
func GetGenerator(framework string) (Generator, error) {
	switch framework {
	case "fastmcp-python":
		return python.NewGenerator(), nil
	case "mcp-go":
		return golang.NewGenerator(), nil
	case "typescript":
		return typescript.NewGenerator(), nil
	case "java":
		return java.NewGenerator(), nil
	default:
		return nil, fmt.Errorf("unsupported framework: %s", framework)
	}
}
