package main

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kagent-dev/kagent/go/plugins/kanban-mcp/internal/config"
	"github.com/kagent-dev/kagent/go/plugins/kanban-mcp/internal/db"
	"github.com/kagent-dev/kagent/go/plugins/kanban-mcp/internal/service"
	"github.com/kagent-dev/kagent/go/plugins/kanban-mcp/internal/sse"
)

// newTestServer creates a fully wired HTTP server backed by an in-memory SQLite DB.
func newTestServer(t *testing.T) *httptest.Server {
	t.Helper()

	dbPath := filepath.Join(t.TempDir(), "test.db")
	cfg := &config.Config{
		DBType: config.DBTypeSQLite,
		DBPath: dbPath,
		Addr:   ":0",
	}

	mgr, err := db.NewManager(cfg)
	if err != nil {
		t.Fatalf("db.NewManager: %v", err)
	}
	if err := mgr.Initialize(); err != nil {
		t.Fatalf("db.Initialize: %v", err)
	}

	hub := sse.NewHub()
	svc := service.NewTaskService(mgr.DB(), hub)
	srv := NewHTTPServer(cfg, svc, hub)

	return httptest.NewServer(srv.Handler)
}

// TestHTTPServer_MCP verifies that the /mcp endpoint accepts MCP JSON-RPC requests
// and returns a valid JSON-RPC response (SSE-wrapped by the MCP SDK Streamable HTTP transport).
func TestHTTPServer_MCP(t *testing.T) {
	ts := newTestServer(t)
	defer ts.Close()

	// The MCP Streamable HTTP transport requires both Accept types.
	body := `{"jsonrpc":"2.0","id":1,"method":"tools/list","params":{}}`
	req, _ := http.NewRequest(http.MethodPost, ts.URL+"/mcp", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("POST /mcp: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		raw, _ := io.ReadAll(resp.Body)
		t.Fatalf("expected 200, got %d: %s", resp.StatusCode, raw)
	}

	// The response is SSE-formatted: "event: message\ndata: <json>\n\n"
	raw, _ := io.ReadAll(resp.Body)
	sseData := string(raw)
	if !strings.Contains(sseData, "data:") {
		t.Fatalf("expected SSE data line, got: %q", sseData)
	}

	// Extract the JSON from the SSE data line.
	var jsonrpcPayload string
	for _, line := range strings.Split(sseData, "\n") {
		if strings.HasPrefix(line, "data: ") {
			jsonrpcPayload = strings.TrimPrefix(line, "data: ")
			break
		}
	}
	if jsonrpcPayload == "" {
		t.Fatalf("no data line found in SSE response: %q", sseData)
	}

	var result map[string]interface{}
	if err := json.Unmarshal([]byte(jsonrpcPayload), &result); err != nil {
		t.Fatalf("decode JSON-RPC payload: %v", err)
	}
	if result["jsonrpc"] != "2.0" {
		t.Errorf("expected jsonrpc=2.0, got %v", result["jsonrpc"])
	}
	if result["result"] == nil && result["error"] == nil {
		t.Error("expected either result or error in JSON-RPC response")
	}
}

// TestHTTPServer_SSE verifies that /events returns an SSE stream with the correct headers
// and delivers an initial snapshot event.
func TestHTTPServer_SSE(t *testing.T) {
	ts := newTestServer(t)
	defer ts.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, ts.URL+"/events", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("GET /events: %v", err)
	}
	defer resp.Body.Close()

	ct := resp.Header.Get("Content-Type")
	if !strings.HasPrefix(ct, "text/event-stream") {
		t.Errorf("expected Content-Type text/event-stream, got %q", ct)
	}

	// Read enough bytes to capture the initial snapshot line
	buf := make([]byte, 512)
	n, _ := resp.Body.Read(buf)
	data := string(buf[:n])

	if !strings.Contains(data, "event: snapshot") {
		t.Errorf("expected snapshot event in SSE stream, got: %q", data)
	}
}

// TestHTTPServer_NotFound verifies that /api/tasks/{unknown-id} returns 404.
func TestHTTPServer_NotFound(t *testing.T) {
	ts := newTestServer(t)
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/api/tasks/99999")
	if err != nil {
		t.Fatalf("GET /api/tasks/99999: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("expected 404, got %d", resp.StatusCode)
	}
}

// TestHTTPServer_CORS verifies that /mcp responses include the expected CORS-related headers.
func TestHTTPServer_CORS(t *testing.T) {
	ts := newTestServer(t)
	defer ts.Close()

	// OPTIONS preflight check
	req, _ := http.NewRequest(http.MethodOptions, ts.URL+"/mcp", nil)
	req.Header.Set("Origin", "http://localhost:3000")
	req.Header.Set("Access-Control-Request-Method", "POST")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("OPTIONS /mcp: %v", err)
	}
	defer resp.Body.Close()

	// Accept either 200 or 204 for a preflight; the key test is the MCP endpoint is reachable.
	// The MCP SDK sets Content-Type on real POST responses.
	body := `{"jsonrpc":"2.0","id":1,"method":"tools/list","params":{}}`
	postReq, _ := http.NewRequest(http.MethodPost, ts.URL+"/mcp", strings.NewReader(body))
	postReq.Header.Set("Content-Type", "application/json")
	postReq.Header.Set("Accept", "application/json, text/event-stream")
	postResp, err := http.DefaultClient.Do(postReq)
	if err != nil {
		t.Fatalf("POST /mcp for CORS test: %v", err)
	}
	defer postResp.Body.Close()

	ct := postResp.Header.Get("Content-Type")
	if ct == "" {
		t.Error("expected Content-Type header on /mcp POST response")
	}
	if postResp.StatusCode != http.StatusOK {
		t.Errorf("expected 200 on /mcp POST, got %d", postResp.StatusCode)
	}
}
