package mcp_test

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/kagent-dev/kagent/go/plugins/temporal-mcp/internal/temporal"
	temporalmcp "github.com/kagent-dev/kagent/go/plugins/temporal-mcp/internal/mcp"
	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

// mockTC is a test double implementing temporal.WorkflowClient.
type mockTC struct {
	workflows []*temporal.WorkflowSummary
	detail    *temporal.WorkflowDetail
	listErr   error
	getErr    error
	cancelErr error
	signalErr error
	canceled  []string
	signaled  []struct{ id, name string }
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

func (m *mockTC) SignalWorkflow(_ context.Context, id, name string, _ interface{}) error {
	if m.signalErr != nil {
		return m.signalErr
	}
	m.signaled = append(m.signaled, struct{ id, name string }{id, name})
	return nil
}

func setupTest(t *testing.T, tc temporal.WorkflowClient) (*mcpsdk.ClientSession, func()) {
	t.Helper()

	server := temporalmcp.NewServer(tc)

	ctx := context.Background()
	st, ct := mcpsdk.NewInMemoryTransports()

	_, err := server.Connect(ctx, st, nil)
	if err != nil {
		t.Fatalf("server.Connect: %v", err)
	}

	client := mcpsdk.NewClient(&mcpsdk.Implementation{Name: "test-client", Version: "v0.0.1"}, nil)
	cs, err := client.Connect(ctx, ct, nil)
	if err != nil {
		t.Fatalf("client.Connect: %v", err)
	}

	return cs, func() { cs.Close() }
}

func callTool(t *testing.T, cs *mcpsdk.ClientSession, name string, args map[string]interface{}) *mcpsdk.CallToolResult {
	t.Helper()
	result, err := cs.CallTool(context.Background(), &mcpsdk.CallToolParams{
		Name:      name,
		Arguments: args,
	})
	if err != nil {
		t.Fatalf("CallTool(%s): %v", name, err)
	}
	return result
}

func extractText(t *testing.T, result *mcpsdk.CallToolResult) string {
	t.Helper()
	if len(result.Content) == 0 {
		t.Fatal("result has no content")
	}
	tc, ok := result.Content[0].(*mcpsdk.TextContent)
	if !ok {
		t.Fatalf("content[0] is not *TextContent")
	}
	return tc.Text
}

func TestMCPTool_ListWorkflows(t *testing.T) {
	now := time.Now()
	tc := &mockTC{
		workflows: []*temporal.WorkflowSummary{
			{WorkflowID: "agent-k8s-agent-abc", AgentName: "k8s-agent", Status: "Running", StartTime: now},
			{WorkflowID: "agent-k8s-agent-def", AgentName: "k8s-agent", Status: "Completed", StartTime: now},
		},
	}
	cs, cleanup := setupTest(t, tc)
	defer cleanup()

	result := callTool(t, cs, "list_workflows", map[string]interface{}{})
	if result.IsError {
		t.Fatalf("list_workflows returned error: %s", extractText(t, result))
	}

	var workflows []*temporal.WorkflowSummary
	if err := json.Unmarshal([]byte(extractText(t, result)), &workflows); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(workflows) != 2 {
		t.Errorf("expected 2 workflows, got %d", len(workflows))
	}
}

func TestMCPTool_ListWorkflows_Error(t *testing.T) {
	tc := &mockTC{listErr: fmt.Errorf("connection refused")}
	cs, cleanup := setupTest(t, tc)
	defer cleanup()

	result := callTool(t, cs, "list_workflows", map[string]interface{}{})
	if !result.IsError {
		t.Error("expected isError for connection failure")
	}
}

func TestMCPTool_GetWorkflow(t *testing.T) {
	now := time.Now()
	tc := &mockTC{
		detail: &temporal.WorkflowDetail{
			WorkflowSummary: temporal.WorkflowSummary{
				WorkflowID: "agent-k8s-agent-abc",
				AgentName:  "k8s-agent",
				Status:     "Running",
				StartTime:  now,
			},
			Activities: []temporal.ActivityInfo{
				{Name: "LLMActivity", Status: "Completed", StartTime: now, Duration: "1.5s"},
			},
		},
	}
	cs, cleanup := setupTest(t, tc)
	defer cleanup()

	result := callTool(t, cs, "get_workflow", map[string]interface{}{
		"workflow_id": "agent-k8s-agent-abc",
	})
	if result.IsError {
		t.Fatalf("get_workflow returned error: %s", extractText(t, result))
	}

	var detail temporal.WorkflowDetail
	if err := json.Unmarshal([]byte(extractText(t, result)), &detail); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if detail.WorkflowID != "agent-k8s-agent-abc" {
		t.Errorf("expected workflow ID agent-k8s-agent-abc, got %q", detail.WorkflowID)
	}
	if len(detail.Activities) != 1 {
		t.Errorf("expected 1 activity, got %d", len(detail.Activities))
	}
}

func TestMCPTool_GetWorkflow_MissingID(t *testing.T) {
	tc := &mockTC{}
	cs, cleanup := setupTest(t, tc)
	defer cleanup()

	// MCP SDK validates required fields — missing workflow_id returns a protocol-level error
	_, err := cs.CallTool(context.Background(), &mcpsdk.CallToolParams{
		Name:      "get_workflow",
		Arguments: map[string]interface{}{},
	})
	if err == nil {
		t.Error("expected error for missing workflow_id")
	}
}

func TestMCPTool_CancelWorkflow(t *testing.T) {
	tc := &mockTC{}
	cs, cleanup := setupTest(t, tc)
	defer cleanup()

	result := callTool(t, cs, "cancel_workflow", map[string]interface{}{
		"workflow_id": "agent-k8s-agent-abc",
	})
	if result.IsError {
		t.Fatalf("cancel_workflow returned error: %s", extractText(t, result))
	}

	if len(tc.canceled) != 1 || tc.canceled[0] != "agent-k8s-agent-abc" {
		t.Errorf("expected cancel call with 'agent-k8s-agent-abc', got %v", tc.canceled)
	}
}

func TestMCPTool_CancelWorkflow_Error(t *testing.T) {
	tc := &mockTC{cancelErr: fmt.Errorf("workflow already completed")}
	cs, cleanup := setupTest(t, tc)
	defer cleanup()

	result := callTool(t, cs, "cancel_workflow", map[string]interface{}{
		"workflow_id": "wf-1",
	})
	if !result.IsError {
		t.Error("expected error for cancel failure")
	}
}

func TestMCPTool_SignalWorkflow(t *testing.T) {
	tc := &mockTC{}
	cs, cleanup := setupTest(t, tc)
	defer cleanup()

	result := callTool(t, cs, "signal_workflow", map[string]interface{}{
		"workflow_id": "agent-k8s-agent-abc",
		"signal_name": "approve",
		"data":        `{"approved": true}`,
	})
	if result.IsError {
		t.Fatalf("signal_workflow returned error: %s", extractText(t, result))
	}

	if len(tc.signaled) != 1 {
		t.Fatalf("expected 1 signal call, got %d", len(tc.signaled))
	}
	if tc.signaled[0].name != "approve" {
		t.Errorf("expected signal name 'approve', got %q", tc.signaled[0].name)
	}
}

func TestMCPTool_SignalWorkflow_MissingName(t *testing.T) {
	tc := &mockTC{}
	cs, cleanup := setupTest(t, tc)
	defer cleanup()

	// MCP SDK validates required fields — missing signal_name returns a protocol-level error
	_, err := cs.CallTool(context.Background(), &mcpsdk.CallToolParams{
		Name:      "signal_workflow",
		Arguments: map[string]interface{}{"workflow_id": "wf-1"},
	})
	if err == nil {
		t.Error("expected error for missing signal_name")
	}
}
