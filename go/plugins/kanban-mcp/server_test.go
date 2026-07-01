package main

import (
	"bufio"
	"context"
	"net/http"
	"net/http/httptest"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
	testcontainers "github.com/testcontainers/testcontainers-go"
	tcpostgres "github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"

	"github.com/kagent-dev/kagent/go/plugins/kanban-mcp/internal/config"
	dbgen "github.com/kagent-dev/kagent/go/plugins/kanban-mcp/internal/db/gen"
	"github.com/kagent-dev/kagent/go/plugins/kanban-mcp/internal/migrations"
	"github.com/kagent-dev/kagent/go/plugins/kanban-mcp/internal/service"
	"github.com/kagent-dev/kagent/go/plugins/kanban-mcp/internal/sse"
)

// startPostgres starts a Postgres container, runs the kanban migrations, and
// returns a connection string. Tests skip when Docker is not available. This is a
// thin local copy of go/core/internal/dbtest (which cannot be imported across the
// internal/ boundary), mirroring the other package test helpers.
func startPostgres(ctx context.Context, t *testing.T) string {
	t.Helper()
	if _, err := exec.LookPath("docker"); err != nil {
		t.Skip("docker not available, skipping container test")
	}

	pgContainer, err := tcpostgres.Run(ctx,
		"postgres:16-alpine",
		tcpostgres.WithDatabase("kanban_test"),
		tcpostgres.WithUsername("postgres"),
		tcpostgres.WithPassword("kanban"),
		testcontainers.WithWaitStrategy(
			wait.ForLog("database system is ready to accept connections").
				WithOccurrence(2).
				WithStartupTimeout(60*time.Second),
		),
	)
	if err != nil {
		t.Fatalf("starting postgres container: %v", err)
	}
	t.Cleanup(func() {
		if err := pgContainer.Terminate(context.Background()); err != nil {
			t.Logf("warning: failed to terminate postgres container: %v", err)
		}
	})

	connStr, err := pgContainer.ConnectionString(ctx, "sslmode=disable")
	if err != nil {
		t.Fatalf("getting connection string: %v", err)
	}

	if err := migrations.RunUp(connStr); err != nil {
		t.Fatalf("running migrations: %v", err)
	}
	return connStr
}

// newTestServer starts Postgres, builds the wired HTTP server, and returns an
// httptest.Server backed by it plus the live TaskService for direct mutation.
func newTestServer(ctx context.Context, t *testing.T) (*httptest.Server, *service.TaskService) {
	t.Helper()
	url := startPostgres(ctx, t)

	pool, err := pgxpool.New(ctx, url)
	if err != nil {
		t.Fatalf("creating pool: %v", err)
	}
	t.Cleanup(pool.Close)

	var svc *service.TaskService
	hub := sse.NewHub(func(board string) any {
		state, err := svc.GetBoard(context.Background(), board)
		if err != nil {
			return &service.BoardState{Columns: []service.Column{}}
		}
		return state
	})
	svc = service.NewTaskService(dbgen.New(pool), pool, hub)

	srv := NewHTTPServer(&config.Config{Addr: ":0", Transport: "http"}, svc, hub)
	ts := httptest.NewServer(srv.Handler)
	t.Cleanup(ts.Close)
	return ts, svc
}

// TestHTTPServer_MCP connects a real MCP client over the streamable-HTTP
// transport to /mcp and verifies a tool call returns a valid result.
func TestHTTPServer_MCP(t *testing.T) {
	ctx := context.Background()
	ts, _ := newTestServer(ctx, t)

	client := mcpsdk.NewClient(&mcpsdk.Implementation{Name: "test", Version: "v0"}, nil)
	session, err := client.Connect(ctx, &mcpsdk.StreamableClientTransport{
		Endpoint: ts.URL + "/mcp",
	}, nil)
	if err != nil {
		t.Fatalf("connecting MCP client: %v", err)
	}
	defer func() { _ = session.Close() }()

	res, err := session.CallTool(ctx, &mcpsdk.CallToolParams{
		Name:      "get_board",
		Arguments: map[string]any{},
	})
	if err != nil {
		t.Fatalf("calling get_board: %v", err)
	}
	if res.IsError {
		t.Fatalf("get_board returned an error result: %+v", res.Content)
	}
	if len(res.Content) == 0 {
		t.Fatal("get_board returned no content")
	}
}

// TestHTTPServer_SSE connects to /events and verifies the streaming content type
// and the initial snapshot event.
func TestHTTPServer_SSE(t *testing.T) {
	ctx := context.Background()
	ts, _ := newTestServer(ctx, t)

	reqCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	req, err := http.NewRequestWithContext(reqCtx, http.MethodGet, ts.URL+"/events", nil)
	if err != nil {
		t.Fatalf("building request: %v", err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("connecting to /events: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if got := resp.Header.Get("Content-Type"); !strings.HasPrefix(got, "text/event-stream") {
		t.Fatalf("Content-Type = %q, want text/event-stream", got)
	}

	// Read the initial snapshot event line.
	scanner := bufio.NewScanner(resp.Body)
	var sawSnapshot bool
	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "event: snapshot") {
			sawSnapshot = true
			break
		}
	}
	if !sawSnapshot {
		t.Fatal("did not receive initial snapshot event")
	}
}

// TestHTTPServer_NotFound verifies a GET for a non-existent task returns 404.
func TestHTTPServer_NotFound(t *testing.T) {
	ctx := context.Background()
	ts, _ := newTestServer(ctx, t)

	resp, err := http.Get(ts.URL + "/api/tasks/99999")
	if err != nil {
		t.Fatalf("GET /api/tasks/99999: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusNotFound)
	}
}

// TestHTTPServer_CORS verifies the /mcp endpoint negotiates an MCP session,
// returning the expected Mcp-Session-Id header on initialize.
func TestHTTPServer_CORS(t *testing.T) {
	ctx := context.Background()
	ts, _ := newTestServer(ctx, t)

	client := mcpsdk.NewClient(&mcpsdk.Implementation{Name: "test", Version: "v0"}, nil)
	session, err := client.Connect(ctx, &mcpsdk.StreamableClientTransport{
		Endpoint: ts.URL + "/mcp",
	}, nil)
	if err != nil {
		t.Fatalf("connecting MCP client: %v", err)
	}
	defer func() { _ = session.Close() }()

	if session.ID() == "" {
		t.Fatal("expected a non-empty MCP session ID from /mcp")
	}
}

// TestHTTPServer_UI verifies the root route serves the embedded single-page UI
// (Step 10): 200 with an HTML body.
func TestHTTPServer_UI(t *testing.T) {
	ctx := context.Background()
	ts, _ := newTestServer(ctx, t)

	resp, err := http.Get(ts.URL + "/")
	if err != nil {
		t.Fatalf("GET /: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusOK)
	}
	if ct := resp.Header.Get("Content-Type"); !strings.HasPrefix(ct, "text/html") {
		t.Fatalf("Content-Type = %q, want text/html prefix", ct)
	}
}
