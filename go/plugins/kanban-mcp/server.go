package main

import (
	"net/http"

	kanbanapi "github.com/kagent-dev/kagent/go/plugins/kanban-mcp/internal/api"
	"github.com/kagent-dev/kagent/go/plugins/kanban-mcp/internal/config"
	kanbanmcp "github.com/kagent-dev/kagent/go/plugins/kanban-mcp/internal/mcp"
	"github.com/kagent-dev/kagent/go/plugins/kanban-mcp/internal/service"
	"github.com/kagent-dev/kagent/go/plugins/kanban-mcp/internal/sse"
	"github.com/kagent-dev/kagent/go/plugins/kanban-mcp/internal/ui"
	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

// NewHTTPServer constructs the HTTP server with all routes wired.
func NewHTTPServer(cfg *config.Config, svc *service.TaskService, hub *sse.Hub) *http.Server {
	mcpServer := kanbanmcp.NewServer(svc)
	mcpHandler := mcpsdk.NewStreamableHTTPHandler(func(*http.Request) *mcpsdk.Server {
		return mcpServer
	}, nil)

	mux := http.NewServeMux()
	mux.Handle("/mcp", mcpHandler)
	mux.HandleFunc("/events", hub.ServeSSE)
	mux.HandleFunc("/api/tasks", kanbanapi.TasksHandler(svc))
	mux.HandleFunc("/api/tasks/", kanbanapi.TaskHandler(svc))
	mux.HandleFunc("/api/attachments/", kanbanapi.AttachmentHandler(svc))
	mux.HandleFunc("/api/board", kanbanapi.BoardHandler(svc))
	mux.Handle("/", ui.Handler())

	return &http.Server{
		Addr:    cfg.Addr,
		Handler: mux,
	}
}
