package tools

import (
	"context"
	"encoding/json"
	"testing"

	"google.golang.org/adk/agent"
	"google.golang.org/adk/memory"
	"google.golang.org/adk/model"
	adksession "google.golang.org/adk/session"
	"google.golang.org/adk/tool/toolconfirmation"
	"google.golang.org/genai"
)

type mockToolContext struct {
	context.Context
	confirmation           *toolconfirmation.ToolConfirmation
	requestConfirmationErr error
	requestedHint          string
	requestedPayload       any
}

func (m *mockToolContext) UserContent() *genai.Content             { return nil }
func (m *mockToolContext) InvocationID() string                    { return "inv-1" }
func (m *mockToolContext) AgentName() string                       { return "test-agent" }
func (m *mockToolContext) ReadonlyState() adksession.ReadonlyState { return nil }
func (m *mockToolContext) UserID() string                          { return "user-1" }
func (m *mockToolContext) AppName() string                         { return "app-1" }
func (m *mockToolContext) SessionID() string                       { return "sess-1" }
func (m *mockToolContext) Branch() string                          { return "" }
func (m *mockToolContext) Artifacts() agent.Artifacts              { return nil }
func (m *mockToolContext) State() adksession.State                 { return nil }
func (m *mockToolContext) FunctionCallID() string                  { return "fc-1" }
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

func TestAskUserTool_Name(t *testing.T) {
	tool := &AskUserTool{}
	if tool.Name() != "ask_user" {
		t.Errorf("Name() = %q, want %q", tool.Name(), "ask_user")
	}
}

func TestAskUserTool_IsLongRunning(t *testing.T) {
	tool := &AskUserTool{}
	if tool.IsLongRunning() {
		t.Error("IsLongRunning() should be false")
	}
}

func TestAskUserTool_Declaration(t *testing.T) {
	tool := &AskUserTool{}
	decl := tool.Declaration()
	if decl == nil {
		t.Fatal("Declaration() returned nil")
	}
	if decl.Name != "ask_user" {
		t.Errorf("Declaration().Name = %q, want %q", decl.Name, "ask_user")
	}
	if decl.Parameters == nil {
		t.Fatal("Declaration().Parameters is nil")
	}
	questionsSchema, ok := decl.Parameters.Properties["questions"]
	if !ok {
		t.Fatal("Declaration().Parameters missing 'questions' property")
	}
	if questionsSchema.Type != genai.TypeArray {
		t.Errorf("questions type = %v, want TypeArray", questionsSchema.Type)
	}
	if questionsSchema.Items == nil {
		t.Fatal("questions.Items is nil")
	}
	// Verify question property exists in items
	if _, ok := questionsSchema.Items.Properties["question"]; !ok {
		t.Error("question item missing 'question' property")
	}
}

func TestAskUserTool_FirstInvocation(t *testing.T) {
	tool := &AskUserTool{}
	ctx := &mockToolContext{Context: context.Background(), confirmation: nil}
	args := map[string]any{
		"questions": []any{
			map[string]any{"question": "What is your name?"},
			map[string]any{"question": "What is your favorite color?"},
		},
	}

	result, err := tool.Run(ctx, args)
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if result["status"] != "pending" {
		t.Errorf("status = %v, want pending", result["status"])
	}
	if ctx.requestedHint == "" {
		t.Error("RequestConfirmation was not called")
	}
	// Hint should contain the question texts joined by "; "
	if ctx.requestedHint != "What is your name?; What is your favorite color?" {
		t.Errorf("hint = %q, want questions joined by '; '", ctx.requestedHint)
	}
}

func TestAskUserTool_ResumeWithAnswers(t *testing.T) {
	tool := &AskUserTool{}
	answers := []any{
		map[string]any{"answer": []any{"Alice"}},
		map[string]any{"answer": []any{"blue"}},
	}
	ctx := &mockToolContext{
		Context: context.Background(),
		confirmation: &toolconfirmation.ToolConfirmation{
			Confirmed: true,
			Payload:   map[string]any{"answers": answers},
		},
	}
	args := map[string]any{
		"questions": []any{
			map[string]any{"question": "What is your name?"},
			map[string]any{"question": "What is your favorite color?"},
		},
	}

	result, err := tool.Run(ctx, args)
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	resultStr, _ := result["result"].(string)
	if resultStr == "" {
		t.Fatal("result is empty")
	}
	var parsed []map[string]any
	if err := json.Unmarshal([]byte(resultStr), &parsed); err != nil {
		t.Fatalf("result is not valid JSON: %v", err)
	}
	if len(parsed) != 2 {
		t.Fatalf("result len = %d, want 2", len(parsed))
	}
	if parsed[0]["question"] != "What is your name?" {
		t.Errorf("result[0].question = %v", parsed[0]["question"])
	}
}

func TestAskUserTool_ResumeCancelled(t *testing.T) {
	tool := &AskUserTool{}
	ctx := &mockToolContext{
		Context: context.Background(),
		confirmation: &toolconfirmation.ToolConfirmation{
			Confirmed: false,
		},
	}
	args := map[string]any{
		"questions": []any{
			map[string]any{"question": "What is your name?"},
		},
	}

	result, err := tool.Run(ctx, args)
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	resultStr, _ := result["result"].(string)
	var parsed map[string]any
	if err := json.Unmarshal([]byte(resultStr), &parsed); err != nil {
		t.Fatalf("result is not valid JSON: %v", err)
	}
	if parsed["status"] != "cancelled" {
		t.Errorf("status = %v, want cancelled", parsed["status"])
	}
}

func TestAskUserTool_ProcessRequest(t *testing.T) {
	askTool := &AskUserTool{}
	req := &model.LLMRequest{}

	err := askTool.ProcessRequest(nil, req)
	if err != nil {
		t.Fatalf("ProcessRequest() error = %v, want nil", err)
	}

	// Verify the tool was registered in the bookkeeping map.
	if _, ok := req.Tools[askTool.Name()]; !ok {
		t.Error("ProcessRequest() did not register tool in req.Tools")
	}

	// Verify the FunctionDeclaration was added to the LLM config.
	if req.Config == nil {
		t.Fatal("ProcessRequest() did not initialise req.Config")
	}
	found := false
	for _, gt := range req.Config.Tools {
		for _, fd := range gt.FunctionDeclarations {
			if fd.Name == askTool.Name() {
				found = true
			}
		}
	}
	if !found {
		t.Error("ProcessRequest() did not add FunctionDeclaration to req.Config.Tools")
	}

	// Verify duplicate detection.
	err = askTool.ProcessRequest(nil, req)
	if err == nil {
		t.Error("ProcessRequest() should return error on duplicate registration")
	}
}
