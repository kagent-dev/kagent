// Command kanban-mcp is the MCP Kanban server. It resolves configuration, runs
// the kanban Postgres migrations, opens a connection pool, and serves the board.
// In stdio mode it runs the MCP server over stdin/stdout so kagent can register
// it as an MCP server. HTTP transport (REST + SSE + MCP over HTTP) is wired in a
// later step.
package main

import (
	"context"
	"errors"
	"flag"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/kagent-dev/kagent/go/plugins/kanban-mcp/internal/config"
	"github.com/kagent-dev/kagent/go/plugins/kanban-mcp/internal/db"
	dbgen "github.com/kagent-dev/kagent/go/plugins/kanban-mcp/internal/db/gen"
	kmcp "github.com/kagent-dev/kagent/go/plugins/kanban-mcp/internal/mcp"
	"github.com/kagent-dev/kagent/go/plugins/kanban-mcp/internal/migrations"
	"github.com/kagent-dev/kagent/go/plugins/kanban-mcp/internal/seed"
	"github.com/kagent-dev/kagent/go/plugins/kanban-mcp/internal/service"
	"github.com/kagent-dev/kagent/go/plugins/kanban-mcp/internal/sse"
)

// seedBoards parses board definitions from config and upserts them. It is called
// once at startup, after migrations and before serving, for every transport.
func seedBoards(ctx context.Context, cfg *config.Config, svc *service.TaskService) error {
	specs, err := seed.Parse(cfg.Boards, cfg.BoardsFile)
	if err != nil {
		return err
	}
	if len(specs) == 0 {
		return nil
	}
	if err := seed.Apply(ctx, svc, specs); err != nil {
		return err
	}
	log.Printf("kanban-mcp: seeded %d board(s) from configuration", len(specs))
	return nil
}

func main() {
	cfg, err := config.Load()
	if err != nil {
		// `--help` / `-h` is a clean exit, not an error.
		if errors.Is(err, flag.ErrHelp) {
			os.Exit(0)
		}
		log.Fatalf("kanban-mcp: failed to load config: %v", err)
	}

	log.Printf("kanban-mcp config: addr=%s transport=%s db-url-set=%t db-url-file=%q log-level=%s readonly=%t",
		cfg.Addr, cfg.Transport, cfg.DBURL != "", cfg.DBURLFile, cfg.LogLevel, cfg.Readonly)

	if err := run(cfg); err != nil {
		log.Fatalf("kanban-mcp: %v", err)
	}
}

// run resolves the database URL, migrates, connects, and serves the configured
// transport. It is separated from main so the exit path is a single error return.
func run(cfg *config.Config) error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	url, err := db.ResolveURL(cfg.DBURL, cfg.DBURLFile)
	if err != nil {
		return err
	}
	if url == "" {
		return errors.New("no database URL configured: set --db-url or --db-url-file")
	}

	if err := migrations.RunUp(url); err != nil {
		return err
	}

	pool, err := db.Connect(ctx, url)
	if err != nil {
		return err
	}
	defer pool.Close()

	if cfg.Transport == "stdio" {
		// stdio mode has no SSE hub: the board is driven entirely by MCP tools.
		svc := service.NewTaskService(dbgen.New(pool), pool, service.NopBroadcaster{})
		if err := seedBoards(ctx, cfg, svc); err != nil {
			return err
		}
		server := kmcp.NewServer(svc)
		log.Printf("kanban-mcp: serving MCP over stdio")
		if err := server.Run(ctx, &mcpsdk.StdioTransport{}); err != nil {
			return err
		}
		return nil
	}

	// HTTP mode: the SSE hub is the broadcaster, and its snapshot reads back from
	// the service. The service and hub reference each other, so the hub is built
	// first with a closure that captures the service once it is assigned.
	var svc *service.TaskService
	hub := sse.NewHub(func(board string) any {
		state, err := svc.GetBoard(context.Background(), board)
		if err != nil {
			return &service.BoardState{Columns: []service.Column{}}
		}
		return state
	})
	svc = service.NewTaskService(dbgen.New(pool), pool, hub)

	if err := seedBoards(ctx, cfg, svc); err != nil {
		return err
	}

	srv := NewHTTPServer(cfg, svc, hub)

	// Shut the server down cleanly when the context is cancelled (SIGINT/SIGTERM).
	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_ = srv.Shutdown(shutdownCtx)
	}()

	log.Printf("kanban-mcp listening on %s", cfg.Addr)
	if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}
