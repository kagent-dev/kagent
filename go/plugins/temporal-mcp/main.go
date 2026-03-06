package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"

	"github.com/kagent-dev/kagent/go/plugins/temporal-mcp/internal/config"
	temporalmcp "github.com/kagent-dev/kagent/go/plugins/temporal-mcp/internal/mcp"
	"github.com/kagent-dev/kagent/go/plugins/temporal-mcp/internal/sse"
	"github.com/kagent-dev/kagent/go/plugins/temporal-mcp/internal/temporal"
	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("failed to load config: %v", err)
	}

	log.Printf("temporal-mcp config: addr=%s transport=%s temporal=%s namespace=%s poll=%s log=%s",
		cfg.Addr, cfg.Transport, cfg.TemporalHostPort, cfg.TemporalNamespace, cfg.PollInterval, cfg.LogLevel)

	tc, err := temporal.NewClient(cfg.TemporalHostPort, cfg.TemporalNamespace)
	if err != nil {
		log.Fatalf("failed to create Temporal client: %v", err)
	}
	defer tc.Close()

	hub := sse.NewHub(tc, cfg.PollInterval)

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	if cfg.Transport == "stdio" {
		log.Printf("starting in stdio transport mode")
		mcpServer := temporalmcp.NewServer(tc)
		if err := mcpServer.Run(ctx, &mcpsdk.StdioTransport{}); err != nil {
			log.Fatalf("MCP stdio server error: %v", err)
		}
		return
	}

	// HTTP mode — start SSE polling in background
	go hub.Start(ctx)

	srv := NewHTTPServer(cfg, tc, hub)
	log.Printf("temporal-mcp listening on %s", cfg.Addr)

	go func() {
		<-ctx.Done()
		srv.Close() //nolint:errcheck
	}()

	if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatalf("HTTP server error: %v", err)
	}
}
