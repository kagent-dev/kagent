package temporal

import (
	"encoding/json"
	"fmt"
	"time"

	a2atype "github.com/a2aproject/a2a-go/a2a"
	enumspb "go.temporal.io/api/enums/v1"
	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/workflow"
)

const (
	// MaxTurns is the safety bound on the number of LLM turns per single message processing.
	MaxTurns = 100

	// SessionIdleTimeout is how long the workflow waits for a new message before exiting.
	SessionIdleTimeout = 1 * time.Hour

	// DefaultLLMActivityTimeout is the per-activity timeout for LLM invocations.
	DefaultLLMActivityTimeout = 5 * time.Minute

	// DefaultToolActivityTimeout is the per-activity timeout for tool executions.
	DefaultToolActivityTimeout = 10 * time.Minute

	// DefaultSessionActivityTimeout is the per-activity timeout for session operations.
	DefaultSessionActivityTimeout = 30 * time.Second

	// DefaultTaskActivityTimeout is the per-activity timeout for task save operations.
	DefaultTaskActivityTimeout = 30 * time.Second
)

// conversationEntry represents a single turn in the conversation history
// passed between LLM activity invocations within the workflow.
type conversationEntry struct {
	Role       string          `json:"role"`
	Content    string          `json:"content,omitempty"`
	ToolCalls  []ToolCall      `json:"toolCalls,omitempty"`
	ToolCallID string          `json:"toolCallID,omitempty"`
	ToolResult json.RawMessage `json:"toolResult,omitempty"`
}

// AgentExecutionWorkflow is a long-running session workflow.
// It initializes a session, then loops waiting for message signals.
// Each message triggers an LLM+tool processing cycle. The workflow
// stays alive across multiple messages in the same session, producing
// a single workflow execution in the Temporal UI.
//
// Flow:
//  1. Initialize session (activity)
//  2. Drain any buffered message signal (from SignalWithStart)
//  3. Loop: wait for message signal -> LLM+tool cycle -> publish result via NATS -> repeat
//  4. Exit on idle timeout (no messages for SessionIdleTimeout)
func AgentExecutionWorkflow(ctx workflow.Context, req *ExecutionRequest) (*ExecutionResult, error) {
	if req == nil {
		return nil, fmt.Errorf("execution request must not be nil")
	}

	config := extractTemporalConfig(req.Config)

	sessionCtx := workflow.WithActivityOptions(ctx, sessionActivityOptions())
	llmCtx := workflow.WithActivityOptions(ctx, llmActivityOptions(config))
	toolCtx := workflow.WithActivityOptions(ctx, toolActivityOptions(config))
	taskCtx := workflow.WithActivityOptions(ctx, taskActivityOptions())

	// Step 1: Initialize session.
	var activities *Activities
	var sessResp SessionResponse
	err := workflow.ExecuteActivity(sessionCtx, activities.SessionActivity, &SessionRequest{
		AppName:   req.AgentName,
		UserID:    req.UserID,
		SessionID: req.SessionID,
	}).Get(sessionCtx, &sessResp)
	if err != nil {
		return nil, fmt.Errorf("session initialization failed: %w", err)
	}

	// Conversation history persists across messages within the session.
	var history []conversationEntry

	// Message signal channel — receives new user messages.
	msgCh := workflow.GetSignalChannel(ctx, MessageSignalName)

	// Step 2: Drain the initial message from SignalWithStart (or from the req itself).
	// The first message comes either via signal (SignalWithStart) or via req.Message (backward compat).
	var firstMsg MessageSignal
	if msgCh.ReceiveAsync(&firstMsg) {
		// Got message from signal channel (SignalWithStart path).
	} else if len(req.Message) > 0 {
		// Backward compatibility: message in the request itself.
		firstMsg = MessageSignal{
			Message:     req.Message,
			NATSSubject: req.NATSSubject,
		}
	}

	if len(firstMsg.Message) > 0 {
		result, err := processMessage(ctx, llmCtx, toolCtx, taskCtx, activities, req, config, &history, &firstMsg)
		if result != nil || err != nil {
			return result, err
		}
	}

	// Complete signal channel — allows explicit session completion.
	completeCh := workflow.GetSignalChannel(ctx, CompleteSignalName)

	// Step 3: Main loop — wait for new messages, complete signal, or idle timeout.
	for {
		var msg MessageSignal
		timerCtx, cancelTimer := workflow.WithCancel(ctx)
		timer := workflow.NewTimer(timerCtx, SessionIdleTimeout)

		// Create a selector to wait for a message, complete signal, or idle timeout.
		sel := workflow.NewSelector(ctx)

		var gotMessage, gotComplete bool
		sel.AddReceive(msgCh, func(ch workflow.ReceiveChannel, more bool) {
			ch.Receive(ctx, &msg)
			gotMessage = true
		})
		sel.AddReceive(completeCh, func(ch workflow.ReceiveChannel, more bool) {
			var reason string
			ch.Receive(ctx, &reason)
			gotComplete = true
		})
		sel.AddFuture(timer, func(f workflow.Future) {
			// Timer fired — idle timeout reached.
		})

		sel.Select(ctx)
		cancelTimer()

		if gotComplete {
			return &ExecutionResult{
				SessionID: req.SessionID,
				Status:    "completed",
				Reason:    "session completed by user",
			}, nil
		}

		if !gotMessage {
			// Idle timeout — gracefully exit.
			return &ExecutionResult{
				SessionID: req.SessionID,
				Status:    "completed",
				Reason:    "session idle timeout",
			}, nil
		}

		result, err := processMessage(ctx, llmCtx, toolCtx, taskCtx, activities, req, config, &history, &msg)
		if result != nil || err != nil {
			return result, err
		}
	}
}

// processMessage handles a single user message through the LLM+tool loop.
func processMessage(
	ctx workflow.Context,
	llmCtx, toolCtx, taskCtx workflow.Context,
	activities *Activities,
	req *ExecutionRequest,
	config TemporalConfig,
	history *[]conversationEntry,
	msg *MessageSignal,
) (*ExecutionResult, error) {
	// Extract text from A2A message parts for the LLM conversation history.
	userText := extractTextFromA2AMessage(msg.Message)

	// Add user message to conversation history.
	*history = append(*history, conversationEntry{
		Role:    "user",
		Content: userText,
	})

	natsSubject := msg.NATSSubject

	// LLM + tool loop for this message.
	for turn := 0; turn < MaxTurns; turn++ {
		historyBytes, err := json.Marshal(*history)
		if err != nil {
			return nil, fmt.Errorf("failed to serialize history at turn %d: %w", turn, err)
		}

		// Invoke LLM.
		var llmResp LLMResponse
		err = workflow.ExecuteActivity(llmCtx, activities.LLMInvokeActivity, &LLMRequest{
			Config:      req.Config,
			History:     historyBytes,
			NATSSubject: natsSubject,
		}).Get(llmCtx, &llmResp)
		if err != nil {
			return &ExecutionResult{
				SessionID: req.SessionID,
				Status:    "failed",
				Reason:    fmt.Sprintf("LLM invocation failed at turn %d: %s", turn, err.Error()),
			}, nil
		}

		// Terminal response: no tool calls, no agent calls, no HITL.
		if llmResp.Terminal || (len(llmResp.ToolCalls) == 0 && len(llmResp.AgentCalls) == 0 && !llmResp.NeedsApproval) {
			*history = append(*history, conversationEntry{
				Role:    "assistant",
				Content: llmResp.Content,
			})

			// Build A2A task with full history including tool calls/results.
			responseBytes, _ := json.Marshal(llmResp)
			now := workflow.Now(ctx)

			taskHistory := buildA2AHistory(*history)
			agentMsg := a2atype.NewMessage(a2atype.MessageRoleAgent, a2atype.TextPart{Text: llmResp.Content})

			task := &a2atype.Task{
				ID:        a2atype.TaskID(req.SessionID),
				ContextID: req.SessionID,
				History:   taskHistory,
				Status: a2atype.TaskStatus{
					State:     a2atype.TaskStateCompleted,
					Message:   agentMsg,
					Timestamp: &now,
				},
			}
			taskData, _ := json.Marshal(task)
			_ = workflow.ExecuteActivity(taskCtx, activities.SaveTaskActivity, &TaskSaveRequest{
				SessionID: req.SessionID,
				TaskData:  taskData,
			}).Get(taskCtx, nil)

			// Publish completion event via NATS so the executor knows this message is done.
			_ = workflow.ExecuteActivity(taskCtx, activities.PublishCompletionActivity, &PublishCompletionRequest{
				SessionID:   req.SessionID,
				Status:      "completed",
				Response:    responseBytes,
				NATSSubject: natsSubject,
			}).Get(taskCtx, nil)

			// Return to the main loop to wait for the next message (don't exit the workflow).
			return nil, nil
		}

		// Append assistant turn with tool calls to history.
		*history = append(*history, conversationEntry{
			Role:      "assistant",
			Content:   llmResp.Content,
			ToolCalls: llmResp.ToolCalls,
		})

		// Execute tool calls in parallel.
		if len(llmResp.ToolCalls) > 0 {
			toolResults, err := executeToolsInParallel(toolCtx, activities, llmResp.ToolCalls, natsSubject)
			if err != nil {
				return &ExecutionResult{
					SessionID: req.SessionID,
					Status:    "failed",
					Reason:    fmt.Sprintf("tool execution failed at turn %d: %s", turn, err.Error()),
				}, nil
			}

			for _, tr := range toolResults {
				*history = append(*history, conversationEntry{
					Role:       "tool",
					ToolCallID: tr.ToolCallID,
					ToolResult: tr.Result,
				})
			}
		}

		// Handle A2A agent calls as child workflows.
		if len(llmResp.AgentCalls) > 0 {
			childResults, err := executeChildWorkflows(ctx, req, llmResp.AgentCalls, config)
			if err != nil {
				return &ExecutionResult{
					SessionID: req.SessionID,
					Status:    "failed",
					Reason:    fmt.Sprintf("child workflow failed at turn %d: %s", turn, err.Error()),
				}, nil
			}

			for _, cr := range childResults {
				resultBytes, _ := json.Marshal(cr)
				*history = append(*history, conversationEntry{
					Role:       "tool",
					ToolCallID: "agent-" + cr.AgentName,
					ToolResult: resultBytes,
				})
			}
		}

		// HITL approval: block on signal.
		if llmResp.NeedsApproval {
			wfInfo := workflow.GetInfo(ctx)
			_ = workflow.ExecuteActivity(taskCtx, activities.PublishApprovalActivity, &PublishApprovalRequest{
				WorkflowID:  wfInfo.WorkflowExecution.ID,
				RunID:       wfInfo.WorkflowExecution.RunID,
				SessionID:   req.SessionID,
				Message:     llmResp.ApprovalMsg,
				NATSSubject: natsSubject,
			}).Get(taskCtx, nil)

			approvalCh := workflow.GetSignalChannel(ctx, ApprovalSignalName)
			var decision ApprovalDecision
			approvalCh.Receive(ctx, &decision)

			if !decision.Approved {
				_ = workflow.ExecuteActivity(taskCtx, activities.PublishCompletionActivity, &PublishCompletionRequest{
					SessionID:   req.SessionID,
					Status:      "rejected",
					Reason:      decision.Reason,
					NATSSubject: natsSubject,
				}).Get(taskCtx, nil)
				return nil, nil
			}

			*history = append(*history, conversationEntry{
				Role:    "user",
				Content: fmt.Sprintf("[APPROVED] %s", decision.Reason),
			})
		}
	}

	// Safety: exceeded max turns.
	return &ExecutionResult{
		SessionID: req.SessionID,
		Status:    "failed",
		Reason:    fmt.Sprintf("exceeded maximum turns (%d)", MaxTurns),
	}, nil
}

// childWorkflowResult captures the outcome of a child workflow execution.
type childWorkflowResult struct {
	AgentName string          `json:"agentName"`
	Status    string          `json:"status"`
	Response  json.RawMessage `json:"response,omitempty"`
	Reason    string          `json:"reason,omitempty"`
}

// executeChildWorkflows launches child workflows for A2A agent calls in parallel
// and waits for all of them to complete.
func executeChildWorkflows(ctx workflow.Context, parentReq *ExecutionRequest, agentCalls []AgentCall, config TemporalConfig) ([]childWorkflowResult, error) {
	results := make([]childWorkflowResult, len(agentCalls))
	futures := make([]workflow.ChildWorkflowFuture, len(agentCalls))

	for i, ac := range agentCalls {
		childSessionID := ChildWorkflowID(parentReq.SessionID, ac.TargetAgent)
		childNATSSubject := "agent." + ac.TargetAgent + "." + childSessionID + ".stream"

		childOpts := workflow.ChildWorkflowOptions{
			TaskQueue:                TaskQueueForAgent(ac.TargetAgent),
			WorkflowID:               childSessionID,
			WorkflowExecutionTimeout: config.WorkflowTimeout,
			ParentClosePolicy:        enumspb.PARENT_CLOSE_POLICY_TERMINATE,
		}
		childCtx := workflow.WithChildOptions(ctx, childOpts)

		childReq := &ExecutionRequest{
			SessionID:   childSessionID,
			UserID:      parentReq.UserID,
			AgentName:   ac.TargetAgent,
			Message:     ac.Message,
			Config:      parentReq.Config,
			NATSSubject: childNATSSubject,
		}

		futures[i] = workflow.ExecuteChildWorkflow(childCtx, AgentExecutionWorkflow, childReq)
	}

	for i, f := range futures {
		var childResult ExecutionResult
		err := f.Get(ctx, &childResult)
		if err != nil {
			return nil, fmt.Errorf("child workflow for agent %q failed: %w", agentCalls[i].TargetAgent, err)
		}

		results[i] = childWorkflowResult{
			AgentName: agentCalls[i].TargetAgent,
			Status:    childResult.Status,
			Response:  childResult.Response,
			Reason:    childResult.Reason,
		}
	}

	return results, nil
}

// executeToolsInParallel executes multiple tool calls concurrently using workflow goroutines.
func executeToolsInParallel(ctx workflow.Context, activities *Activities, toolCalls []ToolCall, natsSubject string) ([]ToolResponse, error) {
	results := make([]ToolResponse, len(toolCalls))
	errs := make([]error, len(toolCalls))

	futures := make([]workflow.Future, len(toolCalls))
	for i, tc := range toolCalls {
		futures[i] = workflow.ExecuteActivity(ctx, activities.ToolExecuteActivity, &ToolRequest{
			ToolName:    tc.Name,
			ToolCallID:  tc.ID,
			Args:        tc.Args,
			NATSSubject: natsSubject,
		})
	}

	for i, f := range futures {
		err := f.Get(ctx, &results[i])
		if err != nil {
			errs[i] = err
		}
	}

	for _, err := range errs {
		if err != nil {
			return nil, err
		}
	}

	return results, nil
}

// extractTemporalConfig extracts TemporalConfig from the serialized agent config.
// Returns defaults if config cannot be parsed.
func extractTemporalConfig(configBytes []byte) TemporalConfig {
	cfg := DefaultTemporalConfig()

	if len(configBytes) == 0 {
		return cfg
	}

	var wrapper struct {
		Temporal *TemporalConfig `json:"temporal"`
	}
	if err := json.Unmarshal(configBytes, &wrapper); err == nil && wrapper.Temporal != nil {
		if wrapper.Temporal.LLMMaxAttempts > 0 {
			cfg.LLMMaxAttempts = wrapper.Temporal.LLMMaxAttempts
		}
		if wrapper.Temporal.ToolMaxAttempts > 0 {
			cfg.ToolMaxAttempts = wrapper.Temporal.ToolMaxAttempts
		}
		if wrapper.Temporal.WorkflowTimeout > 0 {
			cfg.WorkflowTimeout = wrapper.Temporal.WorkflowTimeout
		}
	}

	return cfg
}

// Activity option builders per activity type.

func sessionActivityOptions() workflow.ActivityOptions {
	return workflow.ActivityOptions{
		StartToCloseTimeout: DefaultSessionActivityTimeout,
		RetryPolicy: &temporal.RetryPolicy{
			InitialInterval:    1 * time.Second,
			MaximumInterval:    10 * time.Second,
			MaximumAttempts:    3,
			BackoffCoefficient: 2.0,
		},
	}
}

func llmActivityOptions(config TemporalConfig) workflow.ActivityOptions {
	return workflow.ActivityOptions{
		StartToCloseTimeout: DefaultLLMActivityTimeout,
		RetryPolicy: &temporal.RetryPolicy{
			InitialInterval:    2 * time.Second,
			MaximumInterval:    2 * time.Minute,
			MaximumAttempts:    int32(config.LLMMaxAttempts),
			BackoffCoefficient: 2.0,
		},
	}
}

func toolActivityOptions(config TemporalConfig) workflow.ActivityOptions {
	return workflow.ActivityOptions{
		StartToCloseTimeout: DefaultToolActivityTimeout,
		HeartbeatTimeout:    1 * time.Minute,
		RetryPolicy: &temporal.RetryPolicy{
			InitialInterval:    1 * time.Second,
			MaximumInterval:    1 * time.Minute,
			MaximumAttempts:    int32(config.ToolMaxAttempts),
			BackoffCoefficient: 2.0,
		},
	}
}

func taskActivityOptions() workflow.ActivityOptions {
	return workflow.ActivityOptions{
		StartToCloseTimeout: DefaultTaskActivityTimeout,
		RetryPolicy: &temporal.RetryPolicy{
			InitialInterval:    1 * time.Second,
			MaximumInterval:    10 * time.Second,
			MaximumAttempts:    3,
			BackoffCoefficient: 2.0,
		},
	}
}

// buildA2AHistory converts the internal conversation history into A2A Messages
// suitable for task persistence. Each entry becomes a properly typed message:
// user text, assistant text, function_call DataParts, and function_response DataParts.
func buildA2AHistory(history []conversationEntry) []*a2atype.Message {
	// Build a mapping from tool call ID to tool name for result entries.
	toolNameByID := make(map[string]string)
	for _, entry := range history {
		for _, tc := range entry.ToolCalls {
			toolNameByID[tc.ID] = tc.Name
		}
	}

	var msgs []*a2atype.Message
	for _, entry := range history {
		switch entry.Role {
		case "user":
			msgs = append(msgs, a2atype.NewMessage(a2atype.MessageRoleUser,
				a2atype.TextPart{Text: entry.Content}))

		case "assistant":
			if len(entry.ToolCalls) > 0 {
				// Emit each tool call as a separate message with function_call metadata.
				for _, tc := range entry.ToolCalls {
					var args map[string]any
					if len(tc.Args) > 0 {
						_ = json.Unmarshal(tc.Args, &args)
					}
					msgs = append(msgs, a2atype.NewMessage(a2atype.MessageRoleAgent,
						a2atype.DataPart{
							Data: map[string]any{
								"id":   tc.ID,
								"name": tc.Name,
								"args": args,
							},
							Metadata: map[string]any{"adk_type": "function_call"},
						}))
				}
			}
			if entry.Content != "" && len(entry.ToolCalls) == 0 {
				msgs = append(msgs, a2atype.NewMessage(a2atype.MessageRoleAgent,
					a2atype.TextPart{Text: entry.Content}))
			}

		case "tool":
			var result any
			if len(entry.ToolResult) > 0 {
				_ = json.Unmarshal(entry.ToolResult, &result)
			}
			toolName := toolNameByID[entry.ToolCallID]
			if toolName == "" {
				toolName = entry.ToolCallID
			}
			msgs = append(msgs, a2atype.NewMessage(a2atype.MessageRoleAgent,
				a2atype.DataPart{
					Data: map[string]any{
						"id":   entry.ToolCallID,
						"name": toolName,
						"response": map[string]any{
							"isError": false,
							"result":  result,
						},
					},
					Metadata: map[string]any{"adk_type": "function_response"},
				}))
		}
	}
	return msgs
}

// extractTextFromA2AMessage extracts the text content from a JSON-encoded A2A Message.
// Falls back to treating the bytes as plain text if parsing fails.
func extractTextFromA2AMessage(msgBytes []byte) string {
	if len(msgBytes) == 0 {
		return ""
	}

	// Try to parse as an A2A Message with structured parts.
	var msg struct {
		Parts []json.RawMessage `json:"parts"`
	}
	if err := json.Unmarshal(msgBytes, &msg); err == nil && len(msg.Parts) > 0 {
		var text string
		for _, raw := range msg.Parts {
			var part struct {
				Kind string `json:"kind"`
				Text string `json:"text"`
			}
			if json.Unmarshal(raw, &part) == nil && part.Kind == "text" {
				text += part.Text
			}
		}
		if text != "" {
			return text
		}
	}

	// Fallback: try as a plain JSON string.
	var plain string
	if json.Unmarshal(msgBytes, &plain) == nil {
		return plain
	}

	// Last resort: use raw bytes as text.
	return string(msgBytes)
}
