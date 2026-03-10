package main

import (
	"net/http"
	"net/http/httputil"
	"net/url"

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
	// Reverse-proxy to the official Temporal Web UI if configured.
	// The Temporal Web UI is configured with TEMPORAL_UI_PUBLIC_PATH=/webui
	// so it natively serves all assets under /webui/ — no path rewriting needed.
	if cfg.WebUIURL != "" {
		webuiTarget, _ := url.Parse(cfg.WebUIURL)
		webuiProxy := &httputil.ReverseProxy{
			Director: func(req *http.Request) {
				req.URL.Scheme = webuiTarget.Scheme
				req.URL.Host = webuiTarget.Host
				// Keep path as-is — Temporal UI expects /webui/ prefix
				req.Host = webuiTarget.Host
			},
		}
		mux.Handle("/webui/", webuiProxy)
	}

	mux.Handle("/", ui.Handler(ui.Config{
		// Link to the proxied Temporal Web UI at /webui/ relative path
		WebUIURL:  func() string { if cfg.WebUIURL != "" { return "webui" }; return "" }(),
		Namespace: cfg.TemporalNamespace,
	}))

	return &http.Server{
		Addr:    cfg.Addr,
		Handler: mux,
	}
}
