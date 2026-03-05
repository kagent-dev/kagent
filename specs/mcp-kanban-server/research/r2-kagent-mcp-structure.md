# R2: MCP Structure in Kagent

## Relevant Files

| File | Purpose |
|------|---------|
| `go/internal/mcp/mcp_handler.go` | Kagent's own MCP server (exposes `list_agents`, `invoke_agent`) |
| `go/internal/httpserver/server.go` | HTTP server with `PathPrefix("/mcp").Handler(mcpHandler)` |
| `go/internal/database/manager.go` | GORM dual SQLite/Postgres manager (reusable pattern) |
| `go/cli/internal/mcp/frameworks/golang/` | CLI templates for scaffolding Go MCP servers |

## MCP SDK Usage Pattern in Kagent
```go
// go/internal/mcp/mcp_handler.go
mcp.AddTool[ListAgentsInput, ListAgentsOutput](server, tool, handler)

handler := mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server {
    return server
}, nil)
```

## HTTP Server Route Pattern (gorilla/mux)
```go
// All API at /api/*
s.router.HandleFunc(APIPathTools, ...)

// MCP at /mcp
s.router.PathPrefix("/mcp").Handler(s.config.MCPHandler)

// UI can be added at /
s.router.PathPrefix("/").Handler(uiHandler)
```

## Middleware Already Supports SSE
`go/internal/httpserver/middleware.go` correctly delegates `http.Flusher` through
wrapped `ResponseWriter` — SSE works out of the box.

## Standalone MCP Server Template
`go/cli/internal/mcp/frameworks/golang/templates/cmd/server/main.go.tmpl`
Shows: flag parsing, stdio vs HTTP mode, `mcp.NewStreamableHTTPHandler`.

This is the pattern to follow for the kanban MCP server.
