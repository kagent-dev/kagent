package main

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/kagent-dev/kagent/go/plugins/temporal-mcp/internal/config"
	"github.com/kagent-dev/kagent/go/plugins/temporal-mcp/internal/sse"
	"github.com/kagent-dev/kagent/go/plugins/temporal-mcp/internal/temporal"
)

// mockWorkflowClient implements temporal.WorkflowClient for testing.
type mockWorkflowClient struct {
	workflows []*temporal.WorkflowSummary
}

func (m *mockWorkflowClient) ListWorkflows(_ context.Context, _ temporal.WorkflowFilter) ([]*temporal.WorkflowSummary, error) {
	return m.workflows, nil
}

func (m *mockWorkflowClient) GetWorkflow(_ context.Context, workflowID string) (*temporal.WorkflowDetail, error) {
	for _, w := range m.workflows {
		if w.WorkflowID == workflowID {
			return &temporal.WorkflowDetail{WorkflowSummary: *w}, nil
		}
	}
	return &temporal.WorkflowDetail{}, nil
}

func (m *mockWorkflowClient) CancelWorkflow(_ context.Context, _ string) error {
	return nil
}

func (m *mockWorkflowClient) SignalWorkflow(_ context.Context, _, _ string, _ interface{}) error {
	return nil
}

func newTestServer(t *testing.T) *httptest.Server {
	t.Helper()

	tc := &mockWorkflowClient{
		workflows: []*temporal.WorkflowSummary{
			{
				WorkflowID: "agent-test-sess1",
				RunID:      "run-1",
				AgentName:  "test",
				SessionID:  "sess1",
				Status:     "Running",
				StartTime:  time.Now().Add(-5 * time.Minute),
			},
		},
	}

	cfg := &config.Config{Addr: ":0"}
	hub := sse.NewHub(tc, 5*time.Second)
	srv := NewHTTPServer(cfg, tc, hub)

	return httptest.NewServer(srv.Handler)
}

func TestHTTPServer_UI(t *testing.T) {
	ts := newTestServer(t)
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/")
	if err != nil {
		t.Fatalf("GET /: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected 200, got %d", resp.StatusCode)
	}
	ct := resp.Header.Get("Content-Type")
	if !strings.HasPrefix(ct, "text/html") {
		t.Errorf("expected text/html, got %q", ct)
	}
	body, _ := io.ReadAll(resp.Body)
	if !strings.Contains(string(body), "Temporal Workflows") {
		t.Error("expected body to contain 'Temporal Workflows'")
	}
}

func TestHTTPServer_APIWorkflows(t *testing.T) {
	ts := newTestServer(t)
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/api/workflows")
	if err != nil {
		t.Fatalf("GET /api/workflows: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected 200, got %d", resp.StatusCode)
	}

	var result map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if result["data"] == nil {
		t.Error("expected 'data' field in response")
	}
}

func TestHTTPServer_APIWorkflowDetail(t *testing.T) {
	ts := newTestServer(t)
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/api/workflows/agent-test-sess1")
	if err != nil {
		t.Fatalf("GET /api/workflows/agent-test-sess1: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected 200, got %d", resp.StatusCode)
	}

	var result map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if result["data"] == nil {
		t.Error("expected 'data' field in response")
	}
}

func TestHTTPServer_MCP(t *testing.T) {
	ts := newTestServer(t)
	defer ts.Close()

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

	raw, _ := io.ReadAll(resp.Body)
	sseData := string(raw)
	if !strings.Contains(sseData, "data:") {
		t.Fatalf("expected SSE data line, got: %q", sseData)
	}

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
}

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

	buf := make([]byte, 512)
	n, _ := resp.Body.Read(buf)
	data := string(buf[:n])

	if !strings.Contains(data, "event: snapshot") {
		t.Errorf("expected snapshot event in SSE stream, got: %q", data)
	}
}
