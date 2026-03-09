package temporal

import (
	"encoding/json"
	"fmt"
	"time"

	enumspb "go.temporal.io/api/enums/v1"
	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/workflow"
)

const (
	// MaxTurns is the safety bound on the number of LLM turns per workflow execution.
	MaxTurns = 100

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

// AgentExecutionWorkflow orchestrates a single agent execution run.
// Each LLM turn and tool call is a separate activity for maximum durability.
//
// Flow:
//  1. Initialize session (activity)
//  2. Loop: LLM invoke -> tool execution -> append events
//  3. Save task on completion
//  4. Return result
func AgentExecutionWorkflow(ctx workflow.Context, req *ExecutionRequest) (*ExecutionResult, error) {
	if req == nil {
		return nil, fmt.Errorf("execution request must not be nil")
	}

	// Parse the temporal config from the agent config for retry policies.
	config := extractTemporalConfig(req.Config)

	// Set up activity options for each activity type.
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

	// Build initial conversation history from the incoming message.
	history := []conversationEntry{
		{Role: "user", Content: string(req.Message)},
	}

	// Step 2: LLM + tool loop.
	for turn := 0; turn < MaxTurns; turn++ {
		// Serialize conversation history for the LLM activity.
		historyBytes, err := json.Marshal(history)
		if err != nil {
			return nil, fmt.Errorf("failed to serialize history at turn %d: %w", turn, err)
		}

		// Invoke LLM.
		var llmResp LLMResponse
		err = workflow.ExecuteActivity(llmCtx, activities.LLMInvokeActivity, &LLMRequest{
			Config:      req.Config,
			History:     historyBytes,
			NATSSubject: req.NATSSubject,
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
			// Append assistant response to history for the final event.
			history = append(history, conversationEntry{
				Role:    "assistant",
				Content: llmResp.Content,
			})

			// Append final event to session.
			eventBytes, _ := json.Marshal(map[string]string{
				"type":    "assistant_message",
				"content": llmResp.Content,
			})
			_ = workflow.ExecuteActivity(taskCtx, activities.AppendEventActivity, &AppendEventRequest{
				SessionID: req.SessionID,
				AppName:   req.AgentName,
				UserID:    req.UserID,
				Event:     eventBytes,
			}).Get(taskCtx, nil)

			// Step 3: Save task.
			responseBytes, _ := json.Marshal(llmResp)
			now := workflow.Now(ctx).Format(time.RFC3339)
			taskData, _ := json.Marshal(map[string]interface{}{
				"id":        req.SessionID,
				"contextId": req.SessionID,
				"status": map[string]interface{}{
					"state": "completed",
					"message": map[string]interface{}{
						"kind": "message",
						"role": "agent",
						"parts": []map[string]interface{}{
							{"kind": "text", "text": llmResp.Content},
						},
					},
					"timestamp": now,
				},
			})
			_ = workflow.ExecuteActivity(taskCtx, activities.SaveTaskActivity, &TaskSaveRequest{
				SessionID: req.SessionID,
				TaskData:  taskData,
			}).Get(taskCtx, nil)

			return &ExecutionResult{
				SessionID: req.SessionID,
				Status:    "completed",
				Response:  responseBytes,
			}, nil
		}

		// Append assistant turn with tool calls to history.
		history = append(history, conversationEntry{
			Role:      "assistant",
			Content:   llmResp.Content,
			ToolCalls: llmResp.ToolCalls,
		})

		// Execute tool calls in parallel.
		if len(llmResp.ToolCalls) > 0 {
			toolResults, err := executeToolsInParallel(toolCtx, activities, llmResp.ToolCalls, req.NATSSubject)
			if err != nil {
				return &ExecutionResult{
					SessionID: req.SessionID,
					Status:    "failed",
					Reason:    fmt.Sprintf("tool execution failed at turn %d: %s", turn, err.Error()),
				}, nil
			}

			// Append tool results to history.
			for _, tr := range toolResults {
				history = append(history, conversationEntry{
					Role:       "tool",
					ToolCallID: tr.ToolCallID,
					ToolResult: tr.Result,
				})
			}
		}

		// Handle A2A agent calls as child workflows on target agent task queues.
		if len(llmResp.AgentCalls) > 0 {
			childResults, err := executeChildWorkflows(ctx, req, llmResp.AgentCalls, config)
			if err != nil {
				return &ExecutionResult{
					SessionID: req.SessionID,
					Status:    "failed",
					Reason:    fmt.Sprintf("child workflow failed at turn %d: %s", turn, err.Error()),
				}, nil
			}

			// Append child results to history as tool-like responses for the LLM.
			for _, cr := range childResults {
				resultBytes, _ := json.Marshal(cr)
				history = append(history, conversationEntry{
					Role:       "tool",
					ToolCallID: "agent-" + cr.AgentName,
					ToolResult: resultBytes,
				})
			}
		}

		// HITL approval: block on signal if the LLM requested human approval.
		if llmResp.NeedsApproval {
			// Publish approval request to NATS so the UI/client knows to prompt the user.
			wfInfo := workflow.GetInfo(ctx)
			_ = workflow.ExecuteActivity(taskCtx, activities.PublishApprovalActivity, &PublishApprovalRequest{
				WorkflowID:  wfInfo.WorkflowExecution.ID,
				RunID:       wfInfo.WorkflowExecution.RunID,
				SessionID:   req.SessionID,
				Message:     llmResp.ApprovalMsg,
				NATSSubject: req.NATSSubject,
			}).Get(taskCtx, nil)

			// Block until a signal is received on the "approval" channel.
			// This is durable: survives pod restarts, waits up to workflow timeout (default 48h).
			approvalCh := workflow.GetSignalChannel(ctx, ApprovalSignalName)
			var decision ApprovalDecision
			approvalCh.Receive(ctx, &decision)

			if !decision.Approved {
				return &ExecutionResult{
					SessionID: req.SessionID,
					Status:    "rejected",
					Reason:    decision.Reason,
				}, nil
			}

			// Approved: add the approval context to history and continue the loop.
			history = append(history, conversationEntry{
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
			Config:      parentReq.Config, // child inherits parent config
			NATSSubject: childNATSSubject,
		}

		futures[i] = workflow.ExecuteChildWorkflow(childCtx, AgentExecutionWorkflow, childReq)
	}

	// Wait for all child workflows to complete.
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

	// Launch each tool call as a workflow goroutine.
	futures := make([]workflow.Future, len(toolCalls))
	for i, tc := range toolCalls {
		futures[i] = workflow.ExecuteActivity(ctx, activities.ToolExecuteActivity, &ToolRequest{
			ToolName:    tc.Name,
			ToolCallID:  tc.ID,
			Args:        tc.Args,
			NATSSubject: natsSubject,
		})
	}

	// Wait for all tool calls to complete.
	for i, f := range futures {
		err := f.Get(ctx, &results[i])
		if err != nil {
			errs[i] = err
		}
	}

	// Check for errors. Tool execution errors in the response (ToolResponse.Error)
	// are not activity errors -- they're returned to the LLM for handling.
	// Only propagate actual activity failures.
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

	// Try to extract just the temporal section from the config.
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
