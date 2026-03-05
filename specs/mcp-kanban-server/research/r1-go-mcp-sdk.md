# R1: Official Go MCP SDK

## Decision: Use `modelcontextprotocol/go-sdk`

Kagent already uses `github.com/modelcontextprotocol/go-sdk` v1.2.0 (in `go/go.mod`).
This is the official SDK co-maintained with Google. Use this — no new dependency needed.

## Key Patterns

### Server creation + tool registration (typed handler)
```go
import "github.com/modelcontextprotocol/go-sdk/mcp"

type CreateTaskInput struct {
    Title  string `json:"title" jsonschema:"task title"`
    Status string `json:"status,omitempty" jsonschema:"initial status"`
}

func handleCreateTask(ctx context.Context, req *mcp.CallToolRequest, input CreateTaskInput) (
    *mcp.CallToolResult, CreateTaskOutput, error,
) {
    // ... create task in DB ...
    return nil, output, nil
}

server := mcp.NewServer(&mcp.Implementation{Name: "kanban", Version: "v1.0.0"}, nil)
mcp.AddTool(server, &mcp.Tool{Name: "create_task", Description: "..."}, handleCreateTask)
```

### HTTP transport (Streamable HTTP — modern, preferred)
```go
handler := mcp.NewStreamableHTTPHandler(func(r *http.Request) *mcp.Server {
    return server
}, nil)
mux.Handle("/mcp", handler)
```

### SSE transport (alternative)
```go
handler := mcp.NewSSEHandler(func(r *http.Request) *mcp.Server {
    return server
}, nil)
mux.Handle("/sse", handler)
```

## Sources
- https://github.com/modelcontextprotocol/go-sdk
- Existing usage: `go/internal/mcp/mcp_handler.go`
- Existing template: `go/cli/internal/mcp/frameworks/golang/templates/cmd/server/main.go.tmpl`
