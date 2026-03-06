package main

import (
	"net/http"

	temporalapi "github.com/kagent-dev/kagent/go/plugins/temporal-mcp/internal/api"
	"github.com/kagent-dev/kagent/go/plugins/temporal-mcp/internal/config"
	temporalmcp "github.com/kagent-dev/kagent/go/plugins/temporal-mcp/internal/mcp"
	"github.com/kagent-dev/kagent/go/plugins/temporal-mcp/internal/sse"
	"github.com/kagent-dev/kagent/go/plugins/temporal-mcp/internal/temporal"
	"github.com/kagent-dev/kagent/go/plugins/temporal-mcp/internal/ui"
	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

// NewHTTPServer constructs the HTTP server with all routes wired.
func NewHTTPServer(cfg *config.Config, tc temporal.WorkflowClient, hub *sse.Hub) *http.Server {
	mcpServer := temporalmcp.NewServer(tc)
	mcpHandler := mcpsdk.NewStreamableHTTPHandler(func(*http.Request) *mcpsdk.Server {
		return mcpServer
	}, nil)

	mux := http.NewServeMux()
	mux.Handle("/mcp", mcpHandler)
	mux.HandleFunc("/events", hub.ServeSSE)
	mux.HandleFunc("/api/workflows", temporalapi.WorkflowsHandler(tc))
	mux.HandleFunc("/api/workflows/", temporalapi.WorkflowHandler(tc))
	mux.Handle("/", ui.Handler())

	return &http.Server{
		Addr:    cfg.Addr,
		Handler: mux,
	}
}
