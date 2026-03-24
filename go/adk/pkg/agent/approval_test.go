package agent

import (
	"context"
	"testing"

	"google.golang.org/adk/agent"
	"google.golang.org/adk/memory"
	adksession "google.golang.org/adk/session"
	"google.golang.org/adk/tool/toolconfirmation"
	"google.golang.org/genai"
)
// mockTool satisfies tool.Tool for testing.
type mockTool struct{ name string }

func (m *mockTool) Name() string        { return m.name }
func (m *mockTool) Description() string { return "" }
func (m *mockTool) IsLongRunning() bool { return false }

// mockToolContext satisfies tool.Context for testing.
type mockToolContext struct {
	context.Context
	confirmation           *toolconfirmation.ToolConfirmation
	requestConfirmationErr error
	requestedHint          string
	requestedPayload       any
}

// agent.ReadonlyContext methods
func (m *mockToolContext) UserContent() *genai.Content             { return nil }
func (m *mockToolContext) InvocationID() string                    { return "inv-1" }
func (m *mockToolContext) AgentName() string                       { return "test-agent" }
func (m *mockToolContext) ReadonlyState() adksession.ReadonlyState { return nil }
func (m *mockToolContext) UserID() string                          { return "user-1" }
func (m *mockToolContext) AppName() string                         { return "app-1" }
func (m *mockToolContext) SessionID() string                       { return "sess-1" }
func (m *mockToolContext) Branch() string                          { return "" }

// agent.CallbackContext methods
func (m *mockToolContext) Artifacts() agent.Artifacts { return nil }
func (m *mockToolContext) State() adksession.State    { return nil }

// tool.Context methods
func (m *mockToolContext) FunctionCallID() string { return "fc-1" }
func (m *mockToolContext) Actions() *adksession.EventActions {
	return &adksession.EventActions{}
}
func (m *mockToolContext) SearchMemory(ctx context.Context, query string) (*memory.SearchResponse, error) {
	return nil, nil
}
func (m *mockToolContext) ToolConfirmation() *toolconfirmation.ToolConfirmation {
	return m.confirmation
}
func (m *mockToolContext) RequestConfirmation(hint string, payload any) error {
	m.requestedHint = hint
	m.requestedPayload = payload
	return m.requestConfirmationErr
}

func TestMakeApprovalCallback_ToolNotInSet(t *testing.T) {
	cb := MakeApprovalCallback(map[string]bool{"other_tool": true})
	ctx := &mockToolContext{Context: context.Background()}
	result, err := cb(ctx, &mockTool{name: "safe_tool"}, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != nil {
		t.Errorf("expected nil result for tool not in approval set, got %v", result)
	}
}

func TestMakeApprovalCallback_FirstInvocation(t *testing.T) {
	cb := MakeApprovalCallback(map[string]bool{"dangerous_tool": true})
	ctx := &mockToolContext{Context: context.Background(), confirmation: nil}

	result, err := cb(ctx, &mockTool{name: "dangerous_tool"}, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result == nil {
		t.Fatal("expected non-nil result for first invocation")
	}
	if result["status"] != "confirmation_requested" {
		t.Errorf("status = %v, want confirmation_requested", result["status"])
	}
	if result["tool"] != "dangerous_tool" {
		t.Errorf("tool = %v, want dangerous_tool", result["tool"])
	}
	if ctx.requestedHint == "" {
		t.Error("RequestConfirmation was not called")
	}
}

func TestMakeApprovalCallback_Approved(t *testing.T) {
	cb := MakeApprovalCallback(map[string]bool{"dangerous_tool": true})
	ctx := &mockToolContext{
		Context: context.Background(),
		confirmation: &toolconfirmation.ToolConfirmation{
			Confirmed: true,
		},
	}

	result, err := cb(ctx, &mockTool{name: "dangerous_tool"}, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != nil {
		t.Errorf("expected nil result for approved tool, got %v", result)
	}
}

func TestMakeApprovalCallback_RejectedWithoutReason(t *testing.T) {
	cb := MakeApprovalCallback(map[string]bool{"dangerous_tool": true})
	ctx := &mockToolContext{
		Context: context.Background(),
		confirmation: &toolconfirmation.ToolConfirmation{
			Confirmed: false,
			Payload:   nil,
		},
	}

	result, err := cb(ctx, &mockTool{name: "dangerous_tool"}, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result == nil {
		t.Fatal("expected non-nil result for rejected tool")
	}
	resultStr, _ := result["result"].(string)
	if resultStr != "Tool call was rejected by user." {
		t.Errorf("result = %q, want %q", resultStr, "Tool call was rejected by user.")
	}
}

func TestMakeApprovalCallback_RejectedWithReason(t *testing.T) {
	cb := MakeApprovalCallback(map[string]bool{"dangerous_tool": true})
	ctx := &mockToolContext{
		Context: context.Background(),
		confirmation: &toolconfirmation.ToolConfirmation{
			Confirmed: false,
			Payload:   map[string]any{"rejection_reason": "policy violation"},
		},
	}

	result, err := cb(ctx, &mockTool{name: "dangerous_tool"}, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result == nil {
		t.Fatal("expected non-nil result")
	}
	resultStr, _ := result["result"].(string)
	expected := "Tool call was rejected by user. Reason: policy violation"
	if resultStr != expected {
		t.Errorf("result = %q, want %q", resultStr, expected)
	}
}
