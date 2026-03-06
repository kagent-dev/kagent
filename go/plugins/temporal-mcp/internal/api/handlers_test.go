package api_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	temporalapi "github.com/kagent-dev/kagent/go/plugins/temporal-mcp/internal/api"
	"github.com/kagent-dev/kagent/go/plugins/temporal-mcp/internal/temporal"
)

type mockTC struct {
	workflows []*temporal.WorkflowSummary
	detail    *temporal.WorkflowDetail
	listErr   error
	getErr    error
	cancelErr error
	signalErr error
	canceled  []string
}

func (m *mockTC) ListWorkflows(_ context.Context, _ temporal.WorkflowFilter) ([]*temporal.WorkflowSummary, error) {
	if m.listErr != nil {
		return nil, m.listErr
	}
	if m.workflows == nil {
		return []*temporal.WorkflowSummary{}, nil
	}
	return m.workflows, nil
}

func (m *mockTC) GetWorkflow(_ context.Context, id string) (*temporal.WorkflowDetail, error) {
	if m.getErr != nil {
		return nil, m.getErr
	}
	if m.detail != nil {
		return m.detail, nil
	}
	return nil, fmt.Errorf("not found: %s", id)
}

func (m *mockTC) CancelWorkflow(_ context.Context, id string) error {
	if m.cancelErr != nil {
		return m.cancelErr
	}
	m.canceled = append(m.canceled, id)
	return nil
}

func (m *mockTC) SignalWorkflow(_ context.Context, _, _ string, _ interface{}) error {
	return m.signalErr
}

func newTestServer(t *testing.T, tc *mockTC) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/api/workflows", temporalapi.WorkflowsHandler(tc))
	mux.HandleFunc("/api/workflows/", temporalapi.WorkflowHandler(tc))
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

func TestREST_ListWorkflows(t *testing.T) {
	now := time.Now()
	tc := &mockTC{
		workflows: []*temporal.WorkflowSummary{
			{WorkflowID: "wf-1", Status: "Running", StartTime: now},
			{WorkflowID: "wf-2", Status: "Completed", StartTime: now},
		},
	}
	srv := newTestServer(t, tc)

	resp, err := http.Get(srv.URL + "/api/workflows")
	if err != nil {
		t.Fatalf("GET /api/workflows: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	var result struct {
		Data []*temporal.WorkflowSummary `json:"data"`
	}
	json.NewDecoder(resp.Body).Decode(&result) //nolint:errcheck
	if len(result.Data) != 2 {
		t.Errorf("expected 2 workflows, got %d", len(result.Data))
	}
}

func TestREST_ListWorkflows_Error(t *testing.T) {
	tc := &mockTC{listErr: fmt.Errorf("connection refused")}
	srv := newTestServer(t, tc)

	resp, err := http.Get(srv.URL + "/api/workflows")
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusInternalServerError {
		t.Errorf("expected 500, got %d", resp.StatusCode)
	}
}

func TestREST_GetWorkflow(t *testing.T) {
	now := time.Now()
	tc := &mockTC{
		detail: &temporal.WorkflowDetail{
			WorkflowSummary: temporal.WorkflowSummary{
				WorkflowID: "wf-1",
				Status:     "Running",
				StartTime:  now,
			},
			Activities: []temporal.ActivityInfo{},
		},
	}
	srv := newTestServer(t, tc)

	resp, err := http.Get(srv.URL + "/api/workflows/wf-1")
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	var result struct {
		Data temporal.WorkflowDetail `json:"data"`
	}
	json.NewDecoder(resp.Body).Decode(&result) //nolint:errcheck
	if result.Data.WorkflowID != "wf-1" {
		t.Errorf("expected wf-1, got %q", result.Data.WorkflowID)
	}
}

func TestREST_CancelWorkflow(t *testing.T) {
	tc := &mockTC{}
	srv := newTestServer(t, tc)

	resp, err := http.Post(srv.URL+"/api/workflows/wf-1/cancel", "", nil)
	if err != nil {
		t.Fatalf("POST cancel: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	if len(tc.canceled) != 1 || tc.canceled[0] != "wf-1" {
		t.Errorf("expected cancel of wf-1, got %v", tc.canceled)
	}
}

func TestREST_SignalWorkflow(t *testing.T) {
	tc := &mockTC{}
	srv := newTestServer(t, tc)

	body := `{"signal_name":"approve","data":{"ok":true}}`
	resp, err := http.Post(srv.URL+"/api/workflows/wf-1/signal", "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatalf("POST signal: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
}

func TestREST_SignalWorkflow_MissingName(t *testing.T) {
	tc := &mockTC{}
	srv := newTestServer(t, tc)

	body := `{"data":"hello"}`
	resp, err := http.Post(srv.URL+"/api/workflows/wf-1/signal", "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatalf("POST signal: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", resp.StatusCode)
	}
}

func TestREST_MethodNotAllowed(t *testing.T) {
	tc := &mockTC{}
	srv := newTestServer(t, tc)

	req, _ := http.NewRequest(http.MethodDelete, srv.URL+"/api/workflows", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("DELETE: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusMethodNotAllowed {
		t.Errorf("expected 405, got %d", resp.StatusCode)
	}
}
