package main

import (
	"net/http"

	cronapi "github.com/kagent-dev/kagent/go/plugins/cron-mcp/internal/api"
	"github.com/kagent-dev/kagent/go/plugins/cron-mcp/internal/config"
	cronmcp "github.com/kagent-dev/kagent/go/plugins/cron-mcp/internal/mcp"
	"github.com/kagent-dev/kagent/go/plugins/cron-mcp/internal/scheduler"
	"github.com/kagent-dev/kagent/go/plugins/cron-mcp/internal/service"
	"github.com/kagent-dev/kagent/go/plugins/cron-mcp/internal/sse"
	"github.com/kagent-dev/kagent/go/plugins/cron-mcp/internal/ui"
	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

// NewHTTPServer constructs the HTTP server with all routes wired.
func NewHTTPServer(cfg *config.Config, svc *service.CronService, hub *sse.Hub, sched *scheduler.Scheduler) *http.Server {
	mcpServer := cronmcp.NewServer(svc, sched)
	mcpHandler := mcpsdk.NewStreamableHTTPHandler(func(*http.Request) *mcpsdk.Server {
		return mcpServer
	}, nil)

	mux := http.NewServeMux()
	mux.Handle("/mcp", mcpHandler)
	mux.HandleFunc("/events", hub.ServeSSE)
	mux.HandleFunc("/api/jobs", cronapi.JobsHandler(svc, sched))
	mux.HandleFunc("/api/jobs/", cronapi.JobHandler(svc, sched))
	mux.HandleFunc("/api/executions/", cronapi.ExecutionHandler(svc))
	mux.HandleFunc("/api/board", cronapi.BoardHandler(svc))
	mux.Handle("/", ui.Handler())

	return &http.Server{
		Addr:    cfg.Addr,
		Handler: mux,
	}
}
