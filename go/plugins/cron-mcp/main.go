package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"

	"github.com/kagent-dev/kagent/go/plugins/cron-mcp/internal/config"
	"github.com/kagent-dev/kagent/go/plugins/cron-mcp/internal/db"
	cronmcp "github.com/kagent-dev/kagent/go/plugins/cron-mcp/internal/mcp"
	"github.com/kagent-dev/kagent/go/plugins/cron-mcp/internal/scheduler"
	"github.com/kagent-dev/kagent/go/plugins/cron-mcp/internal/service"
	"github.com/kagent-dev/kagent/go/plugins/cron-mcp/internal/sse"
	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("failed to load config: %v", err)
	}

	log.Printf("cron-mcp config: addr=%s transport=%s db-type=%s db-path=%s log-level=%s shell=%s",
		cfg.Addr, cfg.Transport, cfg.DBType, cfg.DBPath, cfg.LogLevel, cfg.Shell)

	mgr, err := db.NewManager(cfg)
	if err != nil {
		log.Fatalf("failed to create database manager: %v", err)
	}
	if err := mgr.Initialize(); err != nil {
		log.Fatalf("failed to initialize database: %v", err)
	}
	log.Printf("database initialized")

	hub := sse.NewHub()
	svc := service.NewCronService(mgr.DB(), hub)

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	// Create scheduler
	sched := scheduler.New(svc, cfg.Shell)
	if err := sched.Start(ctx); err != nil {
		log.Fatalf("failed to start scheduler: %v", err)
	}
	defer sched.Stop()

	if cfg.Transport == "stdio" {
		log.Printf("starting in stdio transport mode")
		mcpServer := cronmcp.NewServer(svc, sched)
		if err := mcpServer.Run(ctx, &mcpsdk.StdioTransport{}); err != nil {
			log.Fatalf("MCP stdio server error: %v", err)
		}
		return
	}

	// HTTP mode
	srv := NewHTTPServer(cfg, svc, hub, sched)
	log.Printf("cron-mcp listening on %s", cfg.Addr)

	go func() {
		<-ctx.Done()
		srv.Close() //nolint:errcheck
	}()

	if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatalf("HTTP server error: %v", err)
	}
}
