package main

import (
	"log"

	"github.com/kagent-dev/kagent/go/cmd/kanban-mcp/internal/config"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("failed to load config: %v", err)
	}

	log.Printf("kanban-mcp config: addr=%s transport=%s db-type=%s db-path=%s log-level=%s",
		cfg.Addr, cfg.Transport, cfg.DBType, cfg.DBPath, cfg.LogLevel)
}
