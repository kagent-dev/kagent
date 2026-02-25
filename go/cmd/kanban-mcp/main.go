package main

import (
	"context"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/kagent-dev/kagent/go/cmd/kanban-mcp/internal/config"
	"github.com/kagent-dev/kagent/go/cmd/kanban-mcp/internal/db"
	kanbanmcp "github.com/kagent-dev/kagent/go/cmd/kanban-mcp/internal/mcp"
	"github.com/kagent-dev/kagent/go/cmd/kanban-mcp/internal/service"
	"github.com/kagent-dev/kagent/go/cmd/kanban-mcp/internal/sse"
	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("failed to load config: %v", err)
	}

	log.Printf("kanban-mcp config: addr=%s transport=%s db-type=%s db-path=%s log-level=%s",
		cfg.Addr, cfg.Transport, cfg.DBType, cfg.DBPath, cfg.LogLevel)

	mgr, err := db.NewManager(cfg)
	if err != nil {
		log.Fatalf("failed to create database manager: %v", err)
	}
	if err := mgr.Initialize(); err != nil {
		log.Fatalf("failed to initialize database: %v", err)
	}
	log.Printf("database initialized")

	hub := sse.NewHub()
	svc := service.NewTaskService(mgr.DB(), hub)
	mcpServer := kanbanmcp.NewServer(svc)

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	if cfg.Transport == "stdio" {
		log.Printf("starting in stdio transport mode")
		if err := mcpServer.Run(ctx, &mcpsdk.StdioTransport{}); err != nil {
			log.Fatalf("MCP stdio server error: %v", err)
		}
		return
	}

	// HTTP mode: will be fully wired in Step 7
	log.Printf("HTTP mode not yet implemented; use --transport=stdio")
}
