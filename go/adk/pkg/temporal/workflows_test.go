package temporal

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/suite"
	"go.temporal.io/sdk/testsuite"
)

type WorkflowTestSuite struct {
	suite.Suite
	testsuite.WorkflowTestSuite
	env *testsuite.TestWorkflowEnvironment
	act *Activities // nil-receiver for bound method references in mocks
}

func (s *WorkflowTestSuite) SetupTest() {
	s.env = s.NewTestWorkflowEnvironment()
	s.act = &Activities{}
	s.env.RegisterActivity(s.act)
}

func (s *WorkflowTestSuite) AfterTest(_, _ string) {
	s.env.AssertExpectations(s.T())
}

func TestWorkflowSuite(t *testing.T) {
	suite.Run(t, new(WorkflowTestSuite))
}

// Helper: create a basic execution request.
func basicRequest() *ExecutionRequest {
	return &ExecutionRequest{
		SessionID:   "sess-1",
		UserID:      "user-1",
		AgentName:   "test-agent",
		Message:     []byte("Hello, agent!"),
		Config:      []byte(`{}`),
		NATSSubject: "agent.test-agent.sess-1.stream",
	}
}

// Test: simple single-turn execution (no tool calls).
func (s *WorkflowTestSuite) TestSingleTurnCompletion() {
	req := basicRequest()

	s.env.OnActivity(s.act.SessionActivity, mock.Anything, mock.Anything).
		Return(&SessionResponse{SessionID: "sess-1", Created: true}, nil)

	s.env.OnActivity(s.act.LLMInvokeActivity, mock.Anything, mock.Anything).
		Return(&LLMResponse{
			Content:  "Hello! How can I help you?",
			Terminal: true,
		}, nil)

	s.env.OnActivity(s.act.AppendEventActivity, mock.Anything, mock.Anything).Return(nil)
	s.env.OnActivity(s.act.SaveTaskActivity, mock.Anything, mock.Anything).Return(nil)

	s.env.ExecuteWorkflow(AgentExecutionWorkflow, req)

	s.True(s.env.IsWorkflowCompleted())
	s.NoError(s.env.GetWorkflowError())

	var result ExecutionResult
	s.NoError(s.env.GetWorkflowResult(&result))
	s.Equal("completed", result.Status)
	s.Equal("sess-1", result.SessionID)
}

// Test: multi-turn execution with tool calls.
func (s *WorkflowTestSuite) TestMultiTurnWithToolCalls() {
	req := basicRequest()

	s.env.OnActivity(s.act.SessionActivity, mock.Anything, mock.Anything).
		Return(&SessionResponse{SessionID: "sess-1", Created: true}, nil)

	// First LLM turn: returns tool calls.
	s.env.OnActivity(s.act.LLMInvokeActivity, mock.Anything, mock.Anything).
		Return(&LLMResponse{
			Content: "",
			ToolCalls: []ToolCall{
				{ID: "tc-1", Name: "get_weather", Args: []byte(`{"city":"NYC"}`)},
			},
		}, nil).Once()

	// Tool execution returns result.
	s.env.OnActivity(s.act.ToolExecuteActivity, mock.Anything, mock.Anything).
		Return(&ToolResponse{
			ToolCallID: "tc-1",
			Result:     []byte(`{"temp":"72F"}`),
		}, nil)

	// Second LLM turn: terminal response.
	s.env.OnActivity(s.act.LLMInvokeActivity, mock.Anything, mock.Anything).
		Return(&LLMResponse{
			Content:  "The weather in NYC is 72F.",
			Terminal: true,
		}, nil).Once()

	s.env.OnActivity(s.act.AppendEventActivity, mock.Anything, mock.Anything).Return(nil)
	s.env.OnActivity(s.act.SaveTaskActivity, mock.Anything, mock.Anything).Return(nil)

	s.env.ExecuteWorkflow(AgentExecutionWorkflow, req)

	s.True(s.env.IsWorkflowCompleted())
	s.NoError(s.env.GetWorkflowError())

	var result ExecutionResult
	s.NoError(s.env.GetWorkflowResult(&result))
	s.Equal("completed", result.Status)
}

// Test: parallel tool execution (multiple tools in one turn).
func (s *WorkflowTestSuite) TestParallelToolExecution() {
	req := basicRequest()

	s.env.OnActivity(s.act.SessionActivity, mock.Anything, mock.Anything).
		Return(&SessionResponse{SessionID: "sess-1", Created: true}, nil)

	// LLM returns multiple tool calls.
	s.env.OnActivity(s.act.LLMInvokeActivity, mock.Anything, mock.Anything).
		Return(&LLMResponse{
			ToolCalls: []ToolCall{
				{ID: "tc-1", Name: "get_weather", Args: []byte(`{"city":"NYC"}`)},
				{ID: "tc-2", Name: "get_time", Args: []byte(`{"tz":"EST"}`)},
			},
		}, nil).Once()

	// Both tools execute (order doesn't matter for parallel).
	s.env.OnActivity(s.act.ToolExecuteActivity, mock.Anything, &ToolRequest{
		ToolName:    "get_weather",
		ToolCallID:  "tc-1",
		Args:        []byte(`{"city":"NYC"}`),
		NATSSubject: "agent.test-agent.sess-1.stream",
	}).Return(&ToolResponse{ToolCallID: "tc-1", Result: []byte(`"72F"`)}, nil)

	s.env.OnActivity(s.act.ToolExecuteActivity, mock.Anything, &ToolRequest{
		ToolName:    "get_time",
		ToolCallID:  "tc-2",
		Args:        []byte(`{"tz":"EST"}`),
		NATSSubject: "agent.test-agent.sess-1.stream",
	}).Return(&ToolResponse{ToolCallID: "tc-2", Result: []byte(`"3:00 PM"`)}, nil)

	// Second LLM turn: terminal.
	s.env.OnActivity(s.act.LLMInvokeActivity, mock.Anything, mock.Anything).
		Return(&LLMResponse{Content: "Weather is 72F and time is 3:00 PM.", Terminal: true}, nil).Once()

	s.env.OnActivity(s.act.AppendEventActivity, mock.Anything, mock.Anything).Return(nil)
	s.env.OnActivity(s.act.SaveTaskActivity, mock.Anything, mock.Anything).Return(nil)

	s.env.ExecuteWorkflow(AgentExecutionWorkflow, req)

	s.True(s.env.IsWorkflowCompleted())
	s.NoError(s.env.GetWorkflowError())

	var result ExecutionResult
	s.NoError(s.env.GetWorkflowResult(&result))
	s.Equal("completed", result.Status)
}

// Test: LLM activity failure returns failed status (not workflow error).
func (s *WorkflowTestSuite) TestLLMActivityFailure() {
	req := basicRequest()

	s.env.OnActivity(s.act.SessionActivity, mock.Anything, mock.Anything).
		Return(&SessionResponse{SessionID: "sess-1", Created: true}, nil)

	s.env.OnActivity(s.act.LLMInvokeActivity, mock.Anything, mock.Anything).
		Return(nil, errLLMUnavailable)

	s.env.ExecuteWorkflow(AgentExecutionWorkflow, req)

	s.True(s.env.IsWorkflowCompleted())
	s.NoError(s.env.GetWorkflowError())

	var result ExecutionResult
	s.NoError(s.env.GetWorkflowResult(&result))
	s.Equal("failed", result.Status)
	s.Contains(result.Reason, "LLM invocation failed")
}

// Test: session initialization failure causes workflow error.
func (s *WorkflowTestSuite) TestSessionInitFailure() {
	req := basicRequest()

	s.env.OnActivity(s.act.SessionActivity, mock.Anything, mock.Anything).
		Return(nil, errSessionUnavailable)

	s.env.ExecuteWorkflow(AgentExecutionWorkflow, req)

	s.True(s.env.IsWorkflowCompleted())
	s.Error(s.env.GetWorkflowError())
	s.Contains(s.env.GetWorkflowError().Error(), "session initialization failed")
}

// Test: nil request returns error.
func (s *WorkflowTestSuite) TestNilRequest() {
	s.env.ExecuteWorkflow(AgentExecutionWorkflow, (*ExecutionRequest)(nil))

	s.True(s.env.IsWorkflowCompleted())
	s.Error(s.env.GetWorkflowError())
	s.Contains(s.env.GetWorkflowError().Error(), "execution request must not be nil")
}

// Test: tool activity failure returns failed result.
func (s *WorkflowTestSuite) TestToolActivityFailure() {
	req := basicRequest()

	s.env.OnActivity(s.act.SessionActivity, mock.Anything, mock.Anything).
		Return(&SessionResponse{SessionID: "sess-1", Created: true}, nil)

	s.env.OnActivity(s.act.LLMInvokeActivity, mock.Anything, mock.Anything).
		Return(&LLMResponse{
			ToolCalls: []ToolCall{
				{ID: "tc-1", Name: "dangerous_tool", Args: []byte(`{}`)},
			},
		}, nil)

	s.env.OnActivity(s.act.ToolExecuteActivity, mock.Anything, mock.Anything).
		Return(nil, errToolCrash)

	s.env.ExecuteWorkflow(AgentExecutionWorkflow, req)

	s.True(s.env.IsWorkflowCompleted())
	s.NoError(s.env.GetWorkflowError())

	var result ExecutionResult
	s.NoError(s.env.GetWorkflowResult(&result))
	s.Equal("failed", result.Status)
	s.Contains(result.Reason, "tool execution failed")
}

// Test: tool error in response (non-fatal) gets passed back to LLM.
func (s *WorkflowTestSuite) TestToolErrorInResponsePassedToLLM() {
	req := basicRequest()

	s.env.OnActivity(s.act.SessionActivity, mock.Anything, mock.Anything).
		Return(&SessionResponse{SessionID: "sess-1", Created: true}, nil)

	// First turn: LLM requests a tool.
	s.env.OnActivity(s.act.LLMInvokeActivity, mock.Anything, mock.Anything).
		Return(&LLMResponse{
			ToolCalls: []ToolCall{
				{ID: "tc-1", Name: "flaky_tool", Args: []byte(`{}`)},
			},
		}, nil).Once()

	// Tool returns an error in the response (not an activity error).
	s.env.OnActivity(s.act.ToolExecuteActivity, mock.Anything, mock.Anything).
		Return(&ToolResponse{ToolCallID: "tc-1", Error: "tool returned 404"}, nil)

	// Second turn: LLM sees the tool error and gives a final answer.
	s.env.OnActivity(s.act.LLMInvokeActivity, mock.Anything, mock.Anything).
		Return(&LLMResponse{Content: "Sorry, I couldn't get that data.", Terminal: true}, nil).Once()

	s.env.OnActivity(s.act.AppendEventActivity, mock.Anything, mock.Anything).Return(nil)
	s.env.OnActivity(s.act.SaveTaskActivity, mock.Anything, mock.Anything).Return(nil)

	s.env.ExecuteWorkflow(AgentExecutionWorkflow, req)

	s.True(s.env.IsWorkflowCompleted())
	s.NoError(s.env.GetWorkflowError())

	var result ExecutionResult
	s.NoError(s.env.GetWorkflowResult(&result))
	s.Equal("completed", result.Status)
}

// Test: implicit terminal (no tool calls, not marked terminal).
func (s *WorkflowTestSuite) TestImplicitTerminal() {
	req := basicRequest()

	s.env.OnActivity(s.act.SessionActivity, mock.Anything, mock.Anything).
		Return(&SessionResponse{SessionID: "sess-1", Created: true}, nil)

	// LLM returns content with no tool calls and Terminal=false (implicit terminal).
	s.env.OnActivity(s.act.LLMInvokeActivity, mock.Anything, mock.Anything).
		Return(&LLMResponse{Content: "Here's your answer."}, nil)

	s.env.OnActivity(s.act.AppendEventActivity, mock.Anything, mock.Anything).Return(nil)
	s.env.OnActivity(s.act.SaveTaskActivity, mock.Anything, mock.Anything).Return(nil)

	s.env.ExecuteWorkflow(AgentExecutionWorkflow, req)

	s.True(s.env.IsWorkflowCompleted())
	s.NoError(s.env.GetWorkflowError())

	var result ExecutionResult
	s.NoError(s.env.GetWorkflowResult(&result))
	s.Equal("completed", result.Status)
}

// Test: custom retry config from agent config.
func (s *WorkflowTestSuite) TestCustomRetryConfig() {
	config := map[string]interface{}{
		"temporal": map[string]interface{}{
			"llmMaxAttempts":  10,
			"toolMaxAttempts": 5,
		},
	}
	configBytes, _ := json.Marshal(config)

	req := &ExecutionRequest{
		SessionID:   "sess-1",
		UserID:      "user-1",
		AgentName:   "test-agent",
		Message:     []byte("test"),
		Config:      configBytes,
		NATSSubject: "agent.test-agent.sess-1.stream",
	}

	s.env.OnActivity(s.act.SessionActivity, mock.Anything, mock.Anything).
		Return(&SessionResponse{SessionID: "sess-1", Created: true}, nil)
	s.env.OnActivity(s.act.LLMInvokeActivity, mock.Anything, mock.Anything).
		Return(&LLMResponse{Content: "Done.", Terminal: true}, nil)
	s.env.OnActivity(s.act.AppendEventActivity, mock.Anything, mock.Anything).Return(nil)
	s.env.OnActivity(s.act.SaveTaskActivity, mock.Anything, mock.Anything).Return(nil)

	s.env.ExecuteWorkflow(AgentExecutionWorkflow, req)

	s.True(s.env.IsWorkflowCompleted())
	s.NoError(s.env.GetWorkflowError())

	var result ExecutionResult
	s.NoError(s.env.GetWorkflowResult(&result))
	s.Equal("completed", result.Status)
}

// Test: extractTemporalConfig with valid config.
func TestExtractTemporalConfig(t *testing.T) {
	config := map[string]interface{}{
		"temporal": map[string]interface{}{
			"llmMaxAttempts":  8,
			"toolMaxAttempts": 4,
		},
	}
	configBytes, _ := json.Marshal(config)

	cfg := extractTemporalConfig(configBytes)
	if cfg.LLMMaxAttempts != 8 {
		t.Errorf("expected LLMMaxAttempts=8, got %d", cfg.LLMMaxAttempts)
	}
	if cfg.ToolMaxAttempts != 4 {
		t.Errorf("expected ToolMaxAttempts=4, got %d", cfg.ToolMaxAttempts)
	}
}

// Test: extractTemporalConfig with empty config returns defaults.
func TestExtractTemporalConfigDefaults(t *testing.T) {
	cfg := extractTemporalConfig(nil)
	defaults := DefaultTemporalConfig()
	if cfg.LLMMaxAttempts != defaults.LLMMaxAttempts {
		t.Errorf("expected default LLMMaxAttempts=%d, got %d", defaults.LLMMaxAttempts, cfg.LLMMaxAttempts)
	}
	if cfg.ToolMaxAttempts != defaults.ToolMaxAttempts {
		t.Errorf("expected default ToolMaxAttempts=%d, got %d", defaults.ToolMaxAttempts, cfg.ToolMaxAttempts)
	}
}

// Test: extractTemporalConfig with invalid JSON returns defaults.
func TestExtractTemporalConfigInvalidJSON(t *testing.T) {
	cfg := extractTemporalConfig([]byte("not json"))
	defaults := DefaultTemporalConfig()
	if cfg.LLMMaxAttempts != defaults.LLMMaxAttempts {
		t.Errorf("expected default LLMMaxAttempts, got %d", cfg.LLMMaxAttempts)
	}
}

// Test: HITL approval signal allows workflow to continue.
func (s *WorkflowTestSuite) TestHITLApprovalContinues() {
	req := basicRequest()

	s.env.OnActivity(s.act.SessionActivity, mock.Anything, mock.Anything).
		Return(&SessionResponse{SessionID: "sess-1", Created: true}, nil)

	// First LLM turn: needs approval.
	s.env.OnActivity(s.act.LLMInvokeActivity, mock.Anything, mock.Anything).
		Return(&LLMResponse{
			Content:       "I need to delete a file. Do you approve?",
			NeedsApproval: true,
			ApprovalMsg:   "Delete important-file.txt?",
		}, nil).Once()

	// Publish approval activity.
	s.env.OnActivity(s.act.PublishApprovalActivity, mock.Anything, mock.Anything).Return(nil)

	// Register a callback to send the approval signal after the workflow blocks.
	s.env.RegisterDelayedCallback(func() {
		s.env.SignalWorkflow(ApprovalSignalName, &ApprovalDecision{
			Approved: true,
			Reason:   "User approved the deletion",
		})
	}, 0)

	// Second LLM turn after approval: terminal response.
	s.env.OnActivity(s.act.LLMInvokeActivity, mock.Anything, mock.Anything).
		Return(&LLMResponse{Content: "File deleted successfully.", Terminal: true}, nil).Once()

	s.env.OnActivity(s.act.AppendEventActivity, mock.Anything, mock.Anything).Return(nil)
	s.env.OnActivity(s.act.SaveTaskActivity, mock.Anything, mock.Anything).Return(nil)

	s.env.ExecuteWorkflow(AgentExecutionWorkflow, req)

	s.True(s.env.IsWorkflowCompleted())
	s.NoError(s.env.GetWorkflowError())

	var result ExecutionResult
	s.NoError(s.env.GetWorkflowResult(&result))
	s.Equal("completed", result.Status)
}

// Test: HITL rejection signal stops workflow with "rejected" status.
func (s *WorkflowTestSuite) TestHITLRejectionStopsWorkflow() {
	req := basicRequest()

	s.env.OnActivity(s.act.SessionActivity, mock.Anything, mock.Anything).
		Return(&SessionResponse{SessionID: "sess-1", Created: true}, nil)

	// LLM turn: needs approval.
	s.env.OnActivity(s.act.LLMInvokeActivity, mock.Anything, mock.Anything).
		Return(&LLMResponse{
			Content:       "I need to delete a file.",
			NeedsApproval: true,
			ApprovalMsg:   "Delete important-file.txt?",
		}, nil)

	s.env.OnActivity(s.act.PublishApprovalActivity, mock.Anything, mock.Anything).Return(nil)

	// Send rejection signal.
	s.env.RegisterDelayedCallback(func() {
		s.env.SignalWorkflow(ApprovalSignalName, &ApprovalDecision{
			Approved: false,
			Reason:   "Too dangerous",
		})
	}, 0)

	s.env.ExecuteWorkflow(AgentExecutionWorkflow, req)

	s.True(s.env.IsWorkflowCompleted())
	s.NoError(s.env.GetWorkflowError())

	var result ExecutionResult
	s.NoError(s.env.GetWorkflowResult(&result))
	s.Equal("rejected", result.Status)
	s.Equal("Too dangerous", result.Reason)
}

// Test: HITL approval after tool calls in the same turn.
func (s *WorkflowTestSuite) TestHITLAfterToolCalls() {
	req := basicRequest()

	s.env.OnActivity(s.act.SessionActivity, mock.Anything, mock.Anything).
		Return(&SessionResponse{SessionID: "sess-1", Created: true}, nil)

	// First turn: tool calls + needs approval.
	s.env.OnActivity(s.act.LLMInvokeActivity, mock.Anything, mock.Anything).
		Return(&LLMResponse{
			ToolCalls: []ToolCall{
				{ID: "tc-1", Name: "check_file", Args: []byte(`{"path":"important.txt"}`)},
			},
			NeedsApproval: true,
			ApprovalMsg:   "Found file. Delete it?",
		}, nil).Once()

	s.env.OnActivity(s.act.ToolExecuteActivity, mock.Anything, mock.Anything).
		Return(&ToolResponse{ToolCallID: "tc-1", Result: []byte(`"exists"`)}, nil)

	s.env.OnActivity(s.act.PublishApprovalActivity, mock.Anything, mock.Anything).Return(nil)

	s.env.RegisterDelayedCallback(func() {
		s.env.SignalWorkflow(ApprovalSignalName, &ApprovalDecision{
			Approved: true,
			Reason:   "Go ahead",
		})
	}, 0)

	// Second turn after approval: terminal.
	s.env.OnActivity(s.act.LLMInvokeActivity, mock.Anything, mock.Anything).
		Return(&LLMResponse{Content: "Done.", Terminal: true}, nil).Once()

	s.env.OnActivity(s.act.AppendEventActivity, mock.Anything, mock.Anything).Return(nil)
	s.env.OnActivity(s.act.SaveTaskActivity, mock.Anything, mock.Anything).Return(nil)

	s.env.ExecuteWorkflow(AgentExecutionWorkflow, req)

	s.True(s.env.IsWorkflowCompleted())
	s.NoError(s.env.GetWorkflowError())

	var result ExecutionResult
	s.NoError(s.env.GetWorkflowResult(&result))
	s.Equal("completed", result.Status)
}

// Test: parent workflow starts child workflow for A2A agent call and receives result.
// Child workflow executes inline with mocked activities (no OnWorkflow mock needed).
func (s *WorkflowTestSuite) TestChildWorkflowSuccess() {
	req := basicRequest()

	// Session activity: called by both parent and child.
	s.env.OnActivity(s.act.SessionActivity, mock.Anything, mock.Anything).
		Return(&SessionResponse{SessionID: "sess-1", Created: true}, nil)

	// Parent LLM turn 1: returns an agent call.
	s.env.OnActivity(s.act.LLMInvokeActivity, mock.Anything, mock.Anything).
		Return(&LLMResponse{
			Content: "Let me ask the specialist.",
			AgentCalls: []AgentCall{
				{TargetAgent: "specialist", Message: []byte("What is the answer?")},
			},
		}, nil).Once()

	// Child LLM turn (executes inline): terminal response.
	s.env.OnActivity(s.act.LLMInvokeActivity, mock.Anything, mock.Anything).
		Return(&LLMResponse{Content: "The answer is 42.", Terminal: true}, nil).Once()

	// Parent LLM turn 2 (after child result): terminal.
	s.env.OnActivity(s.act.LLMInvokeActivity, mock.Anything, mock.Anything).
		Return(&LLMResponse{Content: "The specialist says the answer is 42.", Terminal: true}, nil).Once()

	s.env.OnActivity(s.act.AppendEventActivity, mock.Anything, mock.Anything).Return(nil)
	s.env.OnActivity(s.act.SaveTaskActivity, mock.Anything, mock.Anything).Return(nil)

	s.env.ExecuteWorkflow(AgentExecutionWorkflow, req)

	s.True(s.env.IsWorkflowCompleted())
	s.NoError(s.env.GetWorkflowError())

	var result ExecutionResult
	s.NoError(s.env.GetWorkflowResult(&result))
	s.Equal("completed", result.Status)
}

// Test: child workflow failure propagates to parent as failed result.
// Child fails at session init, which is a workflow error that propagates to parent.
func (s *WorkflowTestSuite) TestChildWorkflowFailurePropagates() {
	req := basicRequest()

	// Parent session succeeds.
	s.env.OnActivity(s.act.SessionActivity, mock.Anything, mock.Anything).
		Return(&SessionResponse{SessionID: "sess-1", Created: true}, nil).Once()

	// Parent LLM turn: returns agent call.
	s.env.OnActivity(s.act.LLMInvokeActivity, mock.Anything, mock.Anything).
		Return(&LLMResponse{
			AgentCalls: []AgentCall{
				{TargetAgent: "broken-agent", Message: []byte("help")},
			},
		}, nil)

	// Child session fails (causes child workflow error -> propagates to parent).
	s.env.OnActivity(s.act.SessionActivity, mock.Anything, mock.Anything).
		Return(nil, errSessionUnavailable).Once()

	s.env.ExecuteWorkflow(AgentExecutionWorkflow, req)

	s.True(s.env.IsWorkflowCompleted())
	s.NoError(s.env.GetWorkflowError())

	var result ExecutionResult
	s.NoError(s.env.GetWorkflowResult(&result))
	s.Equal("failed", result.Status)
	s.Contains(result.Reason, "child workflow failed")
}

// Test: parallel child workflows (multiple A2A calls in one turn).
// Both children execute inline with mocked activities.
func (s *WorkflowTestSuite) TestParallelChildWorkflows() {
	req := basicRequest()

	// Session activity: parent + 2 children.
	s.env.OnActivity(s.act.SessionActivity, mock.Anything, mock.Anything).
		Return(&SessionResponse{SessionID: "sess-1", Created: true}, nil)

	// Parent LLM turn 1: two agent calls.
	s.env.OnActivity(s.act.LLMInvokeActivity, mock.Anything, mock.Anything).
		Return(&LLMResponse{
			Content: "Let me consult both experts.",
			AgentCalls: []AgentCall{
				{TargetAgent: "expert-a", Message: []byte("question A")},
				{TargetAgent: "expert-b", Message: []byte("question B")},
			},
		}, nil).Once()

	// Child A LLM turn: terminal.
	s.env.OnActivity(s.act.LLMInvokeActivity, mock.Anything, mock.Anything).
		Return(&LLMResponse{Content: "Answer A", Terminal: true}, nil).Once()

	// Child B LLM turn: terminal.
	s.env.OnActivity(s.act.LLMInvokeActivity, mock.Anything, mock.Anything).
		Return(&LLMResponse{Content: "Answer B", Terminal: true}, nil).Once()

	// Parent LLM turn 2: terminal.
	s.env.OnActivity(s.act.LLMInvokeActivity, mock.Anything, mock.Anything).
		Return(&LLMResponse{Content: "Both experts agree.", Terminal: true}, nil).Once()

	s.env.OnActivity(s.act.AppendEventActivity, mock.Anything, mock.Anything).Return(nil)
	s.env.OnActivity(s.act.SaveTaskActivity, mock.Anything, mock.Anything).Return(nil)

	s.env.ExecuteWorkflow(AgentExecutionWorkflow, req)

	s.True(s.env.IsWorkflowCompleted())
	s.NoError(s.env.GetWorkflowError())

	var result ExecutionResult
	s.NoError(s.env.GetWorkflowResult(&result))
	s.Equal("completed", result.Status)
}

// Test: agent calls combined with tool calls in the same turn.
func (s *WorkflowTestSuite) TestAgentCallsWithToolCalls() {
	req := basicRequest()

	// Session: parent + child.
	s.env.OnActivity(s.act.SessionActivity, mock.Anything, mock.Anything).
		Return(&SessionResponse{SessionID: "sess-1", Created: true}, nil)

	// Parent LLM turn 1: both tool calls and agent calls.
	s.env.OnActivity(s.act.LLMInvokeActivity, mock.Anything, mock.Anything).
		Return(&LLMResponse{
			ToolCalls: []ToolCall{
				{ID: "tc-1", Name: "get_data", Args: []byte(`{}`)},
			},
			AgentCalls: []AgentCall{
				{TargetAgent: "analyzer", Message: []byte("analyze this")},
			},
		}, nil).Once()

	// Tool execution.
	s.env.OnActivity(s.act.ToolExecuteActivity, mock.Anything, mock.Anything).
		Return(&ToolResponse{ToolCallID: "tc-1", Result: []byte(`"data"`)}, nil)

	// Child LLM turn: terminal.
	s.env.OnActivity(s.act.LLMInvokeActivity, mock.Anything, mock.Anything).
		Return(&LLMResponse{Content: "Analysis complete.", Terminal: true}, nil).Once()

	// Parent LLM turn 2: terminal.
	s.env.OnActivity(s.act.LLMInvokeActivity, mock.Anything, mock.Anything).
		Return(&LLMResponse{Content: "All done.", Terminal: true}, nil).Once()

	s.env.OnActivity(s.act.AppendEventActivity, mock.Anything, mock.Anything).Return(nil)
	s.env.OnActivity(s.act.SaveTaskActivity, mock.Anything, mock.Anything).Return(nil)

	s.env.ExecuteWorkflow(AgentExecutionWorkflow, req)

	s.True(s.env.IsWorkflowCompleted())
	s.NoError(s.env.GetWorkflowError())

	var result ExecutionResult
	s.NoError(s.env.GetWorkflowResult(&result))
	s.Equal("completed", result.Status)
}

// Sentinel errors for test mocks (Temporal test suite needs concrete errors).
var (
	errLLMUnavailable     = &testError{"LLM provider unavailable"}
	errSessionUnavailable = &testError{"session service unavailable"}
	errToolCrash          = &testError{"tool executor crashed"}
)

type testError struct{ msg string }

func (e *testError) Error() string { return e.msg }
