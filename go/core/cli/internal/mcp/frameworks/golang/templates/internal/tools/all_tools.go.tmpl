package tools

import (
    "context"
    "github.com/modelcontextprotocol/go-sdk/mcp"
)

func AddToolsToServer(server *mcp.Server) {
    for _, addToolFunc := range toolsToAdd {
        addToolFunc(server)
    }
}

var toolsToAdd []func(server *mcp.Server)

func registerTool[I, O any](tool MCPTool[I, O]) {
    toolsToAdd = append(toolsToAdd, func(server *mcp.Server) {
        mcp.AddTool(server, &mcp.Tool{Name: tool.Name, Description: tool.Description}, tool.Handler)
    })
}

type MCPTool[I, O any] struct {
    Name        string
    Description string
    Handler     func(ctx context.Context, cc *mcp.ServerSession, params *mcp.CallToolParamsFor[I]) (*mcp.CallToolResultFor[O], error)
}
