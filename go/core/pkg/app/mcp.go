package app

import (
	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/kagent-dev/kagent/go/core/internal/mcp"
)

// MCPRegistry is what Options.MCPExtensions may add to core's /mcp server.
type MCPRegistry interface {
	// AddTool needs a name, a handler and an input schema of type object.
	AddTool(tool *mcpsdk.Tool, handler mcpsdk.ToolHandler) error
	// AddResource needs a URI and a handler.
	AddResource(resource *mcpsdk.Resource, handler mcpsdk.ResourceHandler) error
	// AddResourceTemplate needs a URI template and a handler.
	AddResourceTemplate(template *mcpsdk.ResourceTemplate, handler mcpsdk.ResourceHandler) error
}

func mcpExtensions(hook func(MCPRegistry) error) func(mcp.Registry) error {
	if hook == nil {
		return nil
	}
	return func(registry mcp.Registry) error { return hook(registry) }
}
