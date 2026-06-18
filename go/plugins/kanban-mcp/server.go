// HTTP server wiring for the kanban-mcp binary. NewHTTPServer mounts all four
// surfaces on a single port:
//
//   - /mcp     MCP over the streamable-HTTP transport (JSON-RPC).
//   - /events  SSE board stream (initial snapshot + board_update events).
//   - /api/*   REST API. Stubbed with 501 Not Implemented until Step 9, except
//     the GET /api/tasks/{id} not-found path which returns 404 so clients and
//     the UI behave correctly before the full handlers land.
//   - /        Embedded single-page UI served by internal/ui.
//
// It is extracted from main so the wiring can be exercised by httptest in unit
// tests without opening a real listener or database.
package main

import (
	"net/http"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/kagent-dev/kagent/go/plugins/kanban-mcp/internal/api"
	"github.com/kagent-dev/kagent/go/plugins/kanban-mcp/internal/config"
	kmcp "github.com/kagent-dev/kagent/go/plugins/kanban-mcp/internal/mcp"
	"github.com/kagent-dev/kagent/go/plugins/kanban-mcp/internal/service"
	"github.com/kagent-dev/kagent/go/plugins/kanban-mcp/internal/sse"
	"github.com/kagent-dev/kagent/go/plugins/kanban-mcp/internal/ui"
)

// NewHTTPServer builds the *http.Server that serves the MCP, SSE, REST, and UI
// surfaces for the kanban board. The returned server is not yet listening;
// callers invoke ListenAndServe (or pass it to httptest in tests).
func NewHTTPServer(cfg *config.Config, svc *service.TaskService, hub *sse.Hub) *http.Server {
	return &http.Server{
		Addr:    cfg.Addr,
		Handler: newMux(svc, hub, cfg.Readonly),
	}
}

// newMux wires every route onto a fresh ServeMux. When readonly is true the
// embedded board UI is served in read-only mode (the "New Task" button is
// hidden).
func newMux(svc *service.TaskService, hub *sse.Hub, readonly bool) *http.ServeMux {
	mcpServer := kmcp.NewServer(svc)
	mcpHandler := mcpsdk.NewStreamableHTTPHandler(
		func(*http.Request) *mcpsdk.Server { return mcpServer },
		nil,
	)

	mux := http.NewServeMux()
	mux.Handle("/mcp", mcpHandler)
	mux.HandleFunc("/events", hub.ServeSSE)

	// REST API (Step 9).
	mux.HandleFunc("/api/tasks", api.TasksHandler(svc))
	mux.HandleFunc("/api/tasks/", api.TaskHandler(svc))
	mux.HandleFunc("/api/subtasks/", api.SubtaskHandler(svc))
	mux.HandleFunc("/api/attachments/", api.AttachmentHandler(svc))
	mux.HandleFunc("/api/board", api.BoardHandler(svc))
	mux.HandleFunc("/api/boards", api.BoardsHandler(svc))

	// Embedded single-page UI (Step 10).
	mux.Handle("/", ui.Handler(readonly))

	return mux
}
